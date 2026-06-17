package generator

import (
	"bamboo/types"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// buildQualificationContext builds structured qualification state for the prompt builder.
// When cmdCenter has qualificationMode="structured", it reads the lead's tracked
// questions and returns their state. When mode is "free-text" or empty, it returns
// mode="free-text" so the prompt builder falls back to legacy behavior.
func buildQualificationContext(lead *types.Lead, cmdCenter bson.M) QualificationContext {
	if cmdCenter == nil {
		return QualificationContext{Mode: "free-text"}
	}

	mode, _ := cmdCenter["qualificationMode"].(string)
	if mode != "structured" {
		return QualificationContext{Mode: "free-text"}
	}

	if lead == nil {
		return QualificationContext{Mode: "structured"}
	}

	var items []QualificationPromptItem
	for _, q := range lead.QualificationQuestions {
		item := QualificationPromptItem{
			Question: q.Question,
		}
		switch {
		case q.AnsweredAt != nil:
			item.Status = "answered"
			item.Answer = q.Answer
		case q.AskedAt != nil:
			item.Status = "unanswered"
		default:
			item.Status = "not-asked"
		}
		items = append(items, item)
	}

	return QualificationContext{
		Mode:      "structured",
		Questions: items,
	}
}

// seedQualificationQuestions syncs the lead's QualificationQuestions with the
// command center "questions" field. New questions are added (not-asked). Removed
// questions are dropped only if they haven't been answered yet.
//
// Only the "questions" field seeds per-lead tracking. The "highlights",
// "keyInfo", and "priorities" fields are display-only — they're injected in
// their own prompt sections (HIGHLIGHTS, KEY_INFO, PRIORITIES) and should
// NOT be treated as questions to ask the lead.
//
// Returns true if the lead's questions were modified.
func seedQualificationQuestions(lead *types.Lead, cmdCenter bson.M) bool {
	if cmdCenter == nil || lead == nil {
		return false
	}

	// Build a set of existing questions by normalized text for dedup
	existing := make(map[string]bool)
	for _, q := range lead.QualificationQuestions {
		key := normalizeQuestion(q.Question)
		existing[key] = true
	}

	modified := false

	// Only seed from "questions" — the others are display-only
	categories := []struct {
		field    string
		category string
	}{
		{"questions", "qualifications"},
	}

	for _, cat := range categories {
		text, _ := cmdCenter[cat.field].(string)
		if text == "" {
			continue
		}
		parsed := parseQuestionsFromText(text)
		for _, qText := range parsed {
			key := normalizeQuestion(qText)
			if !existing[key] {
				lead.QualificationQuestions = append(lead.QualificationQuestions, types.QualificationQuestion{
					ID:       uuid.New().String(),
					Category: cat.category,
					Question: qText,
				})
				existing[key] = true
				modified = true
			}
		}
	}

	// Remove questions no longer in the "questions" field (only if not answered)
	ccSet := make(map[string]bool)
	for _, cat := range categories {
		text, _ := cmdCenter[cat.field].(string)
		if text == "" {
			continue
		}
		for _, q := range parseQuestionsFromText(text) {
			ccSet[normalizeQuestion(q)] = true
		}
	}
	var kept []types.QualificationQuestion
	for _, q := range lead.QualificationQuestions {
		key := normalizeQuestion(q.Question)
		if ccSet[key] || q.AnsweredAt != nil {
			kept = append(kept, q)
		} else {
			modified = true
		}
	}
	if len(kept) != len(lead.QualificationQuestions) {
		lead.QualificationQuestions = kept
	}

	return modified
}

// detectAskedQuestions checks if the AI response contains key phrases from any
// known qualification question. Returns the IDs of questions that appear to
// have been asked in this response.
func detectAskedQuestions(aiResponse string, lead *types.Lead, cmdCenter bson.M) []string {
	if lead == nil || cmdCenter == nil || aiResponse == "" {
		return nil
	}

	lowerResponse := strings.ToLower(aiResponse)

	var askedIDs []string
	for i, q := range lead.QualificationQuestions {
		// Skip if already marked as asked
		if q.AskedAt != nil {
			continue
		}
		if questionAppearsInText(q.Question, lowerResponse) {
			askedIDs = append(askedIDs, q.ID)
			now := time.Now()
			lead.QualificationQuestions[i].AskedAt = &now
		}
	}

	return askedIDs
}

// detectAnsweredQuestion checks if the inbound message is an answer to a
// pending question (one that was asked but not yet answered). If found, it
// marks the question as answered and returns true.
//
// Guards against false positives:
//   - Skips greetings ("hi", "hey", "yo")
//   - Skips messages that are themselves questions (contain "?")
//   - Skips short filler ("ok", "lol", "yeah")
//   - Verifies the question was actually asked (AskedAt != nil)
//   - Verifies the AI's last reply contained the question's key words
//   - Skips questions that are already answered (AnsweredAt != nil)
func detectAnsweredQuestion(lead *types.Lead, inboundMessage, aiLastReply string) bool {
	if lead == nil || strings.TrimSpace(inboundMessage) == "" {
		return false
	}

	msg := strings.TrimSpace(inboundMessage)
	lowerMsg := strings.ToLower(msg)

	// Skip greetings
	if isGreeting(lowerMsg) {
		return false
	}

	// Skip messages that look like questions (lead is asking, not answering)
	if strings.Contains(msg, "?") {
		return false
	}

	// Skip very short filler messages (under 3 chars)
	if len(msg) < 3 {
		return false
	}

	// Skip common filler that isn't an answer
	fillers := map[string]bool{
		"ok": true, "okay": true, "yeah": true, "yep": true, "yup": true,
		"nah": true, "nope": true, "sure": true, "k": true, "kk": true,
		"thanks": true, "thank you": true, "thx": true, "ty": true,
		"cool": true, "nice": true, "bet": true, "alright": true,
		"got it": true, "sounds good": true, "perfect": true,
		"lol": true, "lmao": true, "haha": true, "ok ok": true,
	}
	if fillers[lowerMsg] {
		return false
	}

	lowerAI := strings.ToLower(aiLastReply)
	now := time.Now()
	for i, q := range lead.QualificationQuestions {
		// Skip questions already answered (defensive)
		if q.AnsweredAt != nil {
			continue
		}
		// Must have been asked (AskedAt set)
		if q.AskedAt == nil {
			continue
		}
		// Verify the AI's last reply actually asked this question
		if !questionAppearsInText(q.Question, lowerAI) {
			continue
		}
		lead.QualificationQuestions[i].Answer = msg
		lead.QualificationQuestions[i].AnsweredAt = &now
		conf := 1.0
		lead.QualificationQuestions[i].Confidence = &conf
		return true
	}

	return false
}

// isGreeting returns true if the message is a casual greeting.
func isGreeting(msg string) bool {
	greetings := []string{"hi", "hey", "hello", "yo", "sup", "what's up", "whats up", "howdy", "hola"}
	msg = strings.TrimSpace(msg)
	for _, g := range greetings {
		if msg == g || strings.HasPrefix(msg, g+" ") || strings.HasPrefix(msg, g+"!") || strings.HasPrefix(msg, g+",") {
			return true
		}
	}
	return false
}

// saveQualificationState persists the lead's QualificationQuestions to MongoDB.
// Uses $set to only update the qualificationQuestions field.
func saveQualificationState(client *mongo.Client, teamID string, lead *types.Lead) error {
	if client == nil || lead == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := client.Database("teams").Collection("leads")
	filter := bson.M{"id": lead.ID, "teamId": teamID}
	update := bson.M{"$set": bson.M{"qualificationQuestions": lead.QualificationQuestions}}

	_, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to save qualification state: %w", err)
	}
	return nil
}

// saveAnsweredQuestion atomically updates a single question's answer in MongoDB.
// This avoids the read-modify-write race condition of saveQualificationState when
// only updating one question's answer. Uses $set with arrayFilters to target
// the specific question by ID.
func saveAnsweredQuestion(client *mongo.Client, teamID string, leadID string, questionID string, answer string, confidence *float64) error {
	if client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := client.Database("teams").Collection("leads")
	filter := bson.M{"id": leadID, "teamId": teamID}

	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"qualificationQuestions.$[q].answer":     answer,
			"qualificationQuestions.$[q].answeredAt":  now,
			"qualificationQuestions.$[q].confidence":  confidence,
		},
	}
	opts := options.Update().SetArrayFilters(options.ArrayFilters{
		Filters: []interface{}{
			bson.M{"q.id": questionID, "q.answeredAt": nil},
		},
	})

	_, err := collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("failed to save answered question: %w", err)
	}
	return nil
}

// saveAskedQuestion atomically marks a question as asked in MongoDB.
func saveAskedQuestion(client *mongo.Client, teamID string, leadID string, questionID string) error {
	if client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := client.Database("teams").Collection("leads")
	filter := bson.M{"id": leadID, "teamId": teamID}

	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"qualificationQuestions.$[q].askedAt": now,
		},
	}
	opts := options.Update().SetArrayFilters(options.ArrayFilters{
		Filters: []interface{}{
			bson.M{"q.id": questionID, "q.askedAt": nil},
		},
	})

	_, err := collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("failed to save asked question: %w", err)
	}
	return nil
}

// parseQuestionsFromText splits a command center questions string into individual questions.
// Handles newline-separated and semicolon-separated formats.
func parseQuestionsFromText(questions string) []string {
	if questions == "" {
		return nil
	}

	var result []string

	lines := strings.Split(questions, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Clean bullet points and numbering
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "• ")
		for i := 1; i <= 10; i++ {
			line = strings.TrimPrefix(line, fmt.Sprintf("%d.", i))
			line = strings.TrimPrefix(line, fmt.Sprintf("%d)", i))
		}
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}

	// If no newlines, try semicolons
	if len(result) == 0 {
		parts := strings.Split(questions, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				result = append(result, part)
			}
		}
	}

	// Cap at 10
	if len(result) > 10 {
		result = result[:10]
	}

	return result
}

// questionAppearsInText checks if a question's key phrases appear in the given text.
// Extracts words > 4 chars from the question and checks if a majority appear.
func questionAppearsInText(question string, lowerText string) bool {
	lowerQ := strings.ToLower(question)
	words := strings.Fields(lowerQ)

	var keyWords []string
	for _, w := range words {
		// Strip punctuation
		w = strings.Trim(w, "?,.:;!\"'")
		if len(w) > 4 {
			keyWords = append(keyWords, w)
		}
	}

	if len(keyWords) == 0 {
		// Short question — fall back to substring check
		trimmed := strings.Trim(lowerQ, "?,.:;!\"' ")
		return len(trimmed) > 3 && strings.Contains(lowerText, trimmed)
	}

	matched := 0
	for _, kw := range keyWords {
		if strings.Contains(lowerText, kw) {
			matched++
		}
	}

	// Require majority of key words
	return matched > 0 && matched >= (len(keyWords)+1)/2
}

// normalizeQuestion lowercases and strips punctuation for dedup matching.
func normalizeQuestion(q string) string {
	q = strings.ToLower(strings.TrimSpace(q))
	q = strings.TrimRight(q, "?,.:;! ")
	return q
}

// NormalizeQualificationDates walks the lead's qualificationQuestions
// and converts any string-format dates to *time.Time. Handles old data
// that was saved as ISO strings (via JSON round-trip through the API
// or the frontend) instead of BSON dates (via the Go mongo-driver).
//
// After this runs, all dates are *time.Time, so subsequent writes
// via saveAnsweredQuestion / saveAskedQuestion persist as BSON dates
// (consistent format going forward). Over time, old string-format
// data gets rewritten as BSON dates whenever the lead is read + written.
//
// Idempotent: safe to call multiple times. Exported (capital N) so
// callers in other packages (e.g., main.go's getLeadByPhone) can
// normalize after loading a lead from MongoDB.
func NormalizeQualificationDates(lead *types.Lead, rawDoc bson.M) {
	if lead == nil || rawDoc == nil {
		return
	}
	questions, _ := rawDoc["qualificationQuestions"].(bson.A)
	if questions == nil {
		return
	}
	for i := range lead.QualificationQuestions {
		if i >= len(questions) {
			break
		}
		qm, _ := questions[i].(bson.M)
		if qm == nil {
			continue
		}
		lead.QualificationQuestions[i].AskedAt = parseFlexibleDate(qm["askedAt"])
		lead.QualificationQuestions[i].AnsweredAt = parseFlexibleDate(qm["answeredAt"])
	}
}

// parseFlexibleDate accepts various date representations that may
// come back from MongoDB or JSON round-trips and returns a
// *time.Time. If the value is nil, missing, or unparseable, returns
// nil (so the field behaves as "not set").
//
// Supported types:
//   - *time.Time, time.Time (Go native — from struct decode)
//   - *primitive.DateTime, primitive.DateTime (BSON date — from bson.M)
//   - string (ISO 8601 / RFC 3339 — from JSON round-trip)
func parseFlexibleDate(v interface{}) *time.Time {
	switch x := v.(type) {
	case *time.Time:
		return x
	case time.Time:
		return &x
	case *primitive.DateTime:
		if x == nil {
			return nil
		}
		t := x.Time()
		return &t
	case primitive.DateTime:
		t := x.Time()
		return &t
	case *string:
		if x == nil {
			return nil
		}
		if t, err := time.Parse(time.RFC3339, *x); err == nil {
			return &t
		}
	case string:
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return &t
		}
	}
	return nil
}

