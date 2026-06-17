package generator

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"bamboo/types"

	"github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
	"go.mongodb.org/mongo-driver/bson"
)

// ParserOutput is the JSON shape returned by the AI lead-response parser.
type ParserOutput struct {
	Budget            string `json:"budget"`
	MoveInDate        string `json:"moveInDate"`
	JobTitle          string `json:"jobTitle"`
	Industry          string `json:"industry"`
	Pets              string `json:"pets"`
	BedroomPreference string `json:"bedroomPreference"`

	// Qualification question answers — extracted when the lead's message
	// answers one of the currently-asked questions. Each answer is matched
	// to a specific lead.QualificationQuestions[i].ID.
	QualificationAnswers []QualificationAnswer `json:"qualificationAnswers"`
}

// QualificationAnswer represents a parsed answer to a single qualification question.
type QualificationAnswer struct {
	QuestionID string  `json:"questionId"` // Matches lead.QualificationQuestions[i].ID
	Answer     string  `json:"answer"`     // Literal text from the lead
	Confidence float64 `json:"confidence"` // 0.0-1.0
}

// buildParserSystemPrompt constructs the parser system prompt. The three
// question lists are interpolated at call time so the model sees the
// exact question IDs and clearly understands which questions are in
// which state (answered / pending / not-asked).
//
//	answeredQuestions: questions the lead has already answered (for context)
//	askedNotAnswered:  questions the AI asked but the lead hasn't answered yet
//	notAskedYet:       questions seeded from cmd-center but never formally asked
func buildParserSystemPrompt(
	answeredQuestions []types.QualificationQuestion,
	askedNotAnswered []types.QualificationQuestion,
	notAskedYet []types.QualificationQuestion,
) string {
	var b strings.Builder
	b.WriteString(`You are a JSON data extractor for lead SMS replies.

RULES
- Return ONLY valid JSON. No markdown, no prose, no explanation.
- Copy the lead's wording literally — never paraphrase or summarize.
- OMIT fields that have no extracted value. Never return empty strings, null, or blank objects.
- Do not invent values. Do not infer information that was not stated.
- "no X" / "none" / "0 X" are meaningful negative answers (confidence 0.85+).
- If nothing can be extracted, return {}.
- Use the EXACT questionId values provided.

PROFILE FIELDS
Include ONLY if explicitly expressed. Copy the lead's wording.

- budget             (e.g. "$1200", "$1000-1500", "under $2000")
- moveInDate         (e.g. "August 1", "next month", "ASAP", "in 2 weeks")
- jobTitle           (e.g. "nurse", "software engineer", "student")
- industry           (e.g. "healthcare", "tech", "education")
- pets               (e.g. "1 dog", "no pets", "2 cats")
- bedroomPreference  (e.g. "1 bedroom", "studio", "2 bedrooms")

PROFILE + QUESTION OVERLAP
When a profile field AND a matching qualification question apply to the same answer, include BOTH:
  - The profile field at the top level (canonical form)
  - A qualificationAnswers entry for the matching question (literal text, exact questionId)

CONFIDENCE
- 0.95+  direct, unambiguous answer
- 0.85   clear but informal/partial
- 0.70   plausible, needs interpretation
- 0.50   vague or uncertain
- Omit  < 0.40 (don't include the answer at all)

EXAMPLES

[1] Profile + Q&A overlap
Asked: "How many people will live there?" (id="q-people")
Lead: "3 of us"
Output:
{
  "qualificationAnswers": [
    {"questionId": "q-people", "answer": "3 of us", "confidence": 0.95}
  ]
}

[2] Negative answer
Asked: "Do you have pets?" (id="q-pets")
Lead: "no pets"
Output:
{
  "pets": "none",
  "qualificationAnswers": [
    {"questionId": "q-pets", "answer": "no pets", "confidence": 0.95}
  ]
}

[3] Greeting only
Asked: "What's your budget?" (id="q-budget")
Lead: "haha sounds cool"
Output: {}
`)

	// ── 3-group question listing ──
	// Group 1: already answered (context only — the AI won't re-output
	// answers for these; they help the AI understand what's been covered).
	// Group 2: asked but not yet answered (lead may answer these now).
	// Group 3: never asked yet (parser can still match if the lead
	// happens to mention the topic).
	if len(answeredQuestions) > 0 {
		b.WriteString("Context — already answered (for reference only, do NOT output answers for these):\n")
		for _, q := range answeredQuestions {
			b.WriteString(`- id="`)
			b.WriteString(q.ID)
			b.WriteString(`" question="`)
			b.WriteString(q.Question)
			b.WriteString(`" -> "`)
			b.WriteString(q.Answer)
			b.WriteString(`" (answered`)
			if q.AnsweredAt != nil {
				b.WriteString(" ")
				b.WriteString(q.AnsweredAt.Format("2006-01-02"))
			}
			b.WriteString(")\n")
		}
		b.WriteString("\n")
	}
	if len(askedNotAnswered) > 0 {
		b.WriteString("Asked but not yet answered (lead may answer in this message):\n")
		for _, q := range askedNotAnswered {
			b.WriteString(`- id="`)
			b.WriteString(q.ID)
			b.WriteString(`" question="`)
			b.WriteString(q.Question)
			b.WriteString("\"\n")
		}
		b.WriteString("\n")
	}
	if len(notAskedYet) > 0 {
		b.WriteString("Not asked yet in this conversation (parser can still match if the lead's message answers it):\n")
		for _, q := range notAskedYet {
			b.WriteString(`- id="`)
			b.WriteString(q.ID)
			b.WriteString(`" question="`)
			b.WriteString(q.Question)
			b.WriteString("\"\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(`Return ONLY valid JSON. No markdown, no prose, no explanation.
Empty object {} if nothing expressed.
Do not invent values. Omit fields you cannot extract with confidence.`)
	return b.String()
}

// cleanParserOutput strips empty/invalid entries from the parser
// output. The model is told to omit empty fields but occasionally
// violates this — this is a safety net.
//
// Filters out qualificationAnswers entries with empty questionId
// or empty answer. Empty profile fields are handled by the caller
// (which checks `if parserOutput.X != ""` before writing).
func cleanParserOutput(out ParserOutput) ParserOutput {
	if len(out.QualificationAnswers) == 0 {
		return out
	}
	filtered := make([]QualificationAnswer, 0, len(out.QualificationAnswers))
	for _, ans := range out.QualificationAnswers {
		if ans.QuestionID != "" && ans.Answer != "" {
			filtered = append(filtered, ans)
		}
	}
	out.QualificationAnswers = filtered
	return out
}

// parseLeadResponse calls the AI to extract structured preferences AND
// qualification question answers from the lead's latest message. Retries
// once on failure. Returns empty ParserOutput if both attempts fail.
// 15s timeout per attempt.
//
// The three question lists (answeredQuestions, askedNotAnswered,
// notAskedYet) are all passed to the parser. The parser is allowed to
// match the lead's answers to groups 2 and 3 (askedNotAnswered +
// notAskedYet). Group 1 (answeredQuestions) is provided as context only
// — the AI should not re-output answers for already-answered questions.
func (g *AIGenerator) parseLeadResponse(
	message string,
	answeredQuestions []types.QualificationQuestion,
	askedNotAnswered []types.QualificationQuestion,
	notAskedYet []types.QualificationQuestion,
) ParserOutput {
	if g.client() == nil {
		return ParserOutput{}
	}

	// Combine groups 2 + 3 for the parser's open questions list
	// (groups the AI is allowed to attach answers to).
	openQuestions := make([]types.QualificationQuestion, 0,
		len(askedNotAnswered)+len(notAskedYet))
	openQuestions = append(openQuestions, askedNotAnswered...)
	openQuestions = append(openQuestions, notAskedYet...)

	schema, err := jsonschema.GenerateSchemaForType(ParserOutput{})
	if err != nil {
		log.Printf("\x1b[33m⚠️ Parser schema generation failed: %v\x1b[0m\n", err)
		return ParserOutput{}
	}

	systemPrompt := buildParserSystemPrompt(answeredQuestions, askedNotAnswered, notAskedYet)

	// ── DEBUG: print the full parser system prompt for inspection ──
	// The parser prompt can be ~1-2k tokens depending on how many open
	// questions there are. Print it all so the user can see exactly
	// what the model is being told. Magenta banner to distinguish
	// from the cyan [DEEPSEEK THINKING] and green ✓ Parser answered.
	log.Printf("\n\x1b[35m═══════════════════════════════════════════\x1b[0m\n")
	log.Printf("\x1b[35m📋 PARSER SYSTEM PROMPT (answered=%d, asked-not-answered=%d, not-asked-yet=%d, %d chars):\x1b[0m\n",
		len(answeredQuestions), len(askedNotAnswered), len(notAskedYet), len(systemPrompt))
	log.Printf("\x1b[35m═══════════════════════════════════════════\x1b[0m\n%s\n", systemPrompt)
	log.Printf("\x1b[35m═══════════════════════════════════════════\x1b[0m\n")

	for attempt := 1; attempt <= 2; attempt++ {
		// 15s timeout to accommodate DeepSeek with thinking mode enabled
		// (the previous 5s was too tight and caused context deadline
		// exceeded errors on the second attempt). Worst case 30s total.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

		req := openai.ChatCompletionRequest{
			Model:               g.modelName,
			Temperature:         0,
			MaxCompletionTokens: 400,
			Messages: []openai.ChatCompletionMessage{
				{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
				{Role: openai.ChatMessageRoleUser, Content: "Extract from: \"" + message + "\""},
			},
		}
		// Only use JSON schema when the model supports it (skipped for DeepSeek)
		if g.useJSONResponse {
			req.ResponseFormat = &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
					Name:   "parser_output",
					Schema: schema,
				},
			}
		}

		resp, err := g.client().CreateChatCompletion(ctx, req)
		cancel()

		if err != nil {
			log.Printf("\x1b[33m⚠️ Parser attempt %d failed: %v\x1b[0m\n", attempt, err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if len(resp.Choices) == 0 {
			log.Printf("\x1b[33m⚠️ Parser attempt %d: no choices in response\x1b[0m\n", attempt)
			time.Sleep(200 * time.Millisecond)
			continue
		}

		var out ParserOutput
		if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &out); err != nil {
			log.Printf("\x1b[33m⚠️ Parser attempt %d: JSON parse error: %v\x1b[0m\n", attempt, err)
			time.Sleep(200 * time.Millisecond)
			continue
		}

		// Safety net: strip empty/invalid entries. The model is told to
		// omit empty fields, but occasionally violates this.
		out = cleanParserOutput(out)

		// ── DEBUG: print the parser output for inspection ──
		// Show the full JSON the model returned. Yellow banner (different
		// from magenta for the prompt) so they're easy to tell apart.
		if data, err := json.MarshalIndent(out, "", "  "); err == nil {
			log.Printf("\n\x1b[33m═══════════════════════════════════════════\x1b[0m\n")
			log.Printf("\x1b[33m📤 PARSER OUTPUT (what the model returned):\x1b[0m\n")
			log.Printf("\x1b[33m═══════════════════════════════════════════\x1b[0m\n%s\n", string(data))
			log.Printf("\x1b[33m═══════════════════════════════════════════\x1b[0m\n")
		}

		return out
	}

	return ParserOutput{}
}

// saveFields atomically persists multiple lead fields to MongoDB.
// Runs fire-and-forget in a goroutine. Errors are logged, not returned.
func (g *AIGenerator) saveFields(teamID, leadID string, fields bson.M) {
	if g.mongoClient == nil || len(fields) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := g.mongoClient.Database("teams").Collection("leads")
	filter := bson.M{"id": leadID, "teamId": teamID}
	update := bson.M{"$set": fields}

	if _, err := collection.UpdateOne(ctx, filter, update); err != nil {
		log.Printf("\x1b[33m⚠️ Failed to save lead fields: %v\x1b[0m\n", err)
	}
}
