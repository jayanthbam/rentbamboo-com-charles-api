package generator

import (
	"strings"
	"testing"

	"bamboo/types"
)

func TestBuildParserSystemPrompt_ContainsOverlapSection(t *testing.T) {
	prompt := buildParserSystemPrompt(nil, nil, nil)
	if !strings.Contains(prompt, "PROFILE + QUESTION OVERLAP") {
		t.Errorf("expected 'PROFILE + QUESTION OVERLAP' section in parser prompt, got:\n%s", prompt)
	}
}

func TestBuildParserSystemPrompt_OverlapExplainsDualOutput(t *testing.T) {
	prompt := buildParserSystemPrompt(nil, nil, nil)
	// The overlap section should mention both profile field + qualificationAnswers
	if !strings.Contains(prompt, "profile field") {
		t.Errorf("overlap section should mention 'profile field', got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "qualificationAnswers") {
		t.Errorf("overlap section should mention 'qualificationAnswers', got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "matching qualification question") {
		t.Errorf("overlap section should reference the matching question, got:\n%s", prompt)
	}
}

func TestBuildParserSystemPrompt_OverlapIncludesNegativeExample(t *testing.T) {
	// The negative-answer example should use pets to be concrete
	prompt := buildParserSystemPrompt(nil, nil, nil)
	if !strings.Contains(prompt, "pets") {
		t.Errorf("overlap section / examples should include pets, got:\n%s", prompt)
	}
	// The negative example should use "no pets" or similar
	if !strings.Contains(prompt, "no pets") {
		t.Errorf("overlap section / examples should include 'no pets' negative example, got:\n%s", prompt)
	}
}

func TestBuildParserSystemPrompt_OpenQuestionsAppear(t *testing.T) {
	askedNotAnswered := []types.QualificationQuestion{
		{ID: "q-pets-123", Question: "Do you have any animals?"},
	}
	notAskedYet := []types.QualificationQuestion{
		{ID: "q-bed-456", Question: "What size apartment?"},
	}
	prompt := buildParserSystemPrompt(nil, askedNotAnswered, notAskedYet)
	// Group 2 (asked but not answered) — labeled
	if !strings.Contains(prompt, "Asked but not yet answered") {
		t.Errorf("expected 'Asked but not yet answered' section, got:\n%s", prompt)
	}
	// Group 3 (not asked yet) — labeled
	if !strings.Contains(prompt, "Not asked yet in this conversation") {
		t.Errorf("expected 'Not asked yet' section, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, `id="q-pets-123"`) {
		t.Errorf("expected question id q-pets-123 in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, `id="q-bed-456"`) {
		t.Errorf("expected question id q-bed-456 in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Do you have any animals?") {
		t.Errorf("expected question text in prompt, got:\n%s", prompt)
	}
}

// TestBuildParserSystemPrompt_AnsweredQuestionsGroup verifies the
// "already answered" group is rendered as context (with answer + date),
// not as something the AI should re-output.
func TestBuildParserSystemPrompt_AnsweredQuestionsGroup(t *testing.T) {
	answered := []types.QualificationQuestion{
		{
			ID:       "q-1",
			Question: "What is your budget?",
			Answer:   "$1200",
		},
	}
	prompt := buildParserSystemPrompt(answered, nil, nil)
	if !strings.Contains(prompt, "already answered") {
		t.Errorf("expected 'already answered' section, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "for reference only") {
		t.Errorf("expected 'for reference only' label, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, `"$1200"`) {
		t.Errorf("expected the previous answer text in context, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "do NOT output answers for these") {
		t.Errorf("expected explicit 'do NOT output' instruction, got:\n%s", prompt)
	}
}

// TestBuildParserSystemPrompt_AllThreeGroups verifies all 3 groups
// render together with correct labels and question IDs.
func TestBuildParserSystemPrompt_AllThreeGroups(t *testing.T) {
	answered := []types.QualificationQuestion{
		{ID: "q-answered", Question: "Already answered question?", Answer: "yes"},
	}
	askedNotAnswered := []types.QualificationQuestion{
		{ID: "q-pending", Question: "Pending question?"},
	}
	notAskedYet := []types.QualificationQuestion{
		{ID: "q-future", Question: "Future question?"},
	}
	prompt := buildParserSystemPrompt(answered, askedNotAnswered, notAskedYet)

	for _, id := range []string{"q-answered", "q-pending", "q-future"} {
		if !strings.Contains(prompt, `id="`+id+`"`) {
			t.Errorf("expected question %s in prompt, got:\n%s", id, prompt)
		}
	}
}

// TestBuildParserSystemPrompt_OmitsEmptyFieldsRule verifies the prompt
// explicitly tells the model to OMIT empty fields (no empty strings,
// null, or blank objects).
func TestBuildParserSystemPrompt_OmitsEmptyFieldsRule(t *testing.T) {
	prompt := buildParserSystemPrompt(nil, nil, nil)
	if !strings.Contains(prompt, "OMIT") {
		t.Errorf("expected 'OMIT' rule in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "empty strings") {
		t.Errorf("expected 'empty strings' warning in prompt, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "null") {
		t.Errorf("expected 'null' warning in prompt, got:\n%s", prompt)
	}
}

// TestBuildParserSystemPrompt_HasConfidenceTable verifies the prompt
// has a confidence table with explicit thresholds.
func TestBuildParserSystemPrompt_HasConfidenceTable(t *testing.T) {
	prompt := buildParserSystemPrompt(nil, nil, nil)
	for _, threshold := range []string{"0.95", "0.85", "0.70", "0.50", "0.40"} {
		if !strings.Contains(prompt, threshold) {
			t.Errorf("expected threshold %s in confidence table, got:\n%s", threshold, prompt)
		}
	}
}

// TestBuildParserSystemPrompt_HasSixProfileFields verifies the prompt
// lists exactly 6 profile fields (no extras, no missing).
func TestBuildParserSystemPrompt_HasSixProfileFields(t *testing.T) {
	prompt := buildParserSystemPrompt(nil, nil, nil)
	expectedFields := []string{
		"budget",
		"moveInDate",
		"jobTitle",
		"industry",
		"pets",
		"bedroomPreference",
	}
	for _, field := range expectedFields {
		if !strings.Contains(prompt, field) {
			t.Errorf("expected profile field %q in prompt, got:\n%s", field, prompt)
		}
	}
	// No additional fields added (e.g., no "occupants" unless user adds it).
	if strings.Contains(prompt, "occupants") {
		t.Errorf("prompt should NOT contain 'occupants' field (was hallucinated by an AI model), got:\n%s", prompt)
	}
}

// TestBuildParserSystemPrompt_NoLongExamples verifies the prompt was
// slimmed down from 6 examples to 3.
func TestBuildParserSystemPrompt_NoLongExamples(t *testing.T) {
	prompt := buildParserSystemPrompt(nil, nil, nil)
	// The old prompt had examples [1] through [6]. The new prompt has
	// examples [1] through [3]. Verify [4], [5], [6] are gone.
	for _, oldMarker := range []string{"[4]", "[5]", "[6]"} {
		if strings.Contains(prompt, oldMarker) {
			t.Errorf("prompt should NOT contain old example marker %s (examples were trimmed from 6 to 3), got:\n%s", oldMarker, prompt)
		}
	}
}

// ---- cleanParserOutput tests ----

func TestCleanParserOutput_StripsEmptyEntries(t *testing.T) {
	in := ParserOutput{
		Budget: "$1200",
		QualificationAnswers: []QualificationAnswer{
			{QuestionID: "q-1", Answer: "yes", Confidence: 0.9},
			{QuestionID: "", Answer: "orphan", Confidence: 0.9},     // empty QuestionID
			{QuestionID: "q-2", Answer: "", Confidence: 0.9},          // empty Answer
			{QuestionID: "q-3", Answer: "valid", Confidence: 0.8},
		},
	}
	out := cleanParserOutput(in)
	if len(out.QualificationAnswers) != 2 {
		t.Errorf("expected 2 valid entries, got %d", len(out.QualificationAnswers))
	}
	if out.QualificationAnswers[0].QuestionID != "q-1" {
		t.Errorf("expected first entry to be q-1, got %s", out.QualificationAnswers[0].QuestionID)
	}
	if out.QualificationAnswers[1].QuestionID != "q-3" {
		t.Errorf("expected second entry to be q-3, got %s", out.QualificationAnswers[1].QuestionID)
	}
}

func TestCleanParserOutput_EmptyArray(t *testing.T) {
	in := ParserOutput{Budget: "$1200"}
	out := cleanParserOutput(in)
	if len(out.QualificationAnswers) != 0 {
		t.Errorf("expected empty array, got %d entries", len(out.QualificationAnswers))
	}
}

func TestCleanParserOutput_AllValid(t *testing.T) {
	in := ParserOutput{
		QualificationAnswers: []QualificationAnswer{
			{QuestionID: "q-1", Answer: "yes", Confidence: 0.9},
			{QuestionID: "q-2", Answer: "no", Confidence: 0.85},
		},
	}
	out := cleanParserOutput(in)
	if len(out.QualificationAnswers) != 2 {
		t.Errorf("expected 2 entries, got %d", len(out.QualificationAnswers))
	}
}
