package generator

import (
	"bamboo/types"
	"encoding/json"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func TestDetectAnsweredQuestion_SkipsGreetings(t *testing.T) {
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q1", Question: "What is your budget?", AskedAt: timePtr(time.Now())},
		},
	}

	greetings := []string{"hi", "hey", "yo", "hello", "sup", "what's up", "howdy"}
	for _, g := range greetings {
		result := detectAnsweredQuestion(lead, g, "What is your budget?")
		if result {
			t.Errorf("expected false for greeting %q, got true", g)
		}
		// Verify question was NOT marked as answered
		if lead.QualificationQuestions[0].AnsweredAt != nil {
			t.Errorf("greeting %q should not mark question as answered", g)
		}
	}
}

func TestDetectAnsweredQuestion_SkipsQuestions(t *testing.T) {
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q1", Question: "What is your budget?", AskedAt: timePtr(time.Now())},
		},
	}

	questions := []string{"what about pets?", "how much is rent?", "do you have tours?"}
	for _, q := range questions {
		result := detectAnsweredQuestion(lead, q, "What is your budget?")
		if result {
			t.Errorf("expected false for question %q, got true", q)
		}
	}
}

func TestDetectAnsweredQuestion_SkipsFiller(t *testing.T) {
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q1", Question: "What is your budget?", AskedAt: timePtr(time.Now())},
		},
	}

	fillers := []string{"ok", "yeah", "lol", "k", "thanks", "cool", "bet", "sure", "nice"}
	for _, f := range fillers {
		result := detectAnsweredQuestion(lead, f, "What is your budget?")
		if result {
			t.Errorf("expected false for filler %q, got true", f)
		}
	}
}

func TestDetectAnsweredQuestion_SkipsShortMessages(t *testing.T) {
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q1", Question: "What is your budget?", AskedAt: timePtr(time.Now())},
		},
	}

	result := detectAnsweredQuestion(lead, "ab", "What is your budget?")
	if result {
		t.Error("expected false for 2-char message, got true")
	}
}

func TestDetectAnsweredQuestion_AcceptsRealAnswer(t *testing.T) {
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q1", Question: "What is your budget?", AskedAt: timePtr(time.Now())},
		},
	}

	result := detectAnsweredQuestion(lead, "around $1200 per month", "What is your budget?")
	if !result {
		t.Error("expected true for real answer, got false")
	}
	if lead.QualificationQuestions[0].Answer != "around $1200 per month" {
		t.Errorf("expected answer 'around $1200 per month', got %q", lead.QualificationQuestions[0].Answer)
	}
	if lead.QualificationQuestions[0].AnsweredAt == nil {
		t.Error("expected AnsweredAt to be set")
	}
}

func TestDetectAnsweredQuestion_SkipsAlreadyAnswered(t *testing.T) {
	now := time.Now()
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q1", Question: "What is your budget?", AskedAt: &now, AnsweredAt: &now, Answer: "$1200"},
			{ID: "q2", Question: "When are you looking to move?", AskedAt: &now},
		},
	}

	result := detectAnsweredQuestion(lead, "next month", "When are you looking to move?")
	if !result {
		t.Error("expected true for second question, got false")
	}
	// Should have answered q2, not q1
	if lead.QualificationQuestions[1].Answer != "next month" {
		t.Errorf("expected answer on q2, got %q", lead.QualificationQuestions[1].Answer)
	}
}

func TestDetectAnsweredQuestion_NoPendingQuestions(t *testing.T) {
	now := time.Now()
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q1", Question: "What is your budget?", AnsweredAt: &now, Answer: "$1200"},
		},
	}

	result := detectAnsweredQuestion(lead, "some message", "")
	if result {
		t.Error("expected false when no pending questions, got true")
	}
}

func TestIsGreeting(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"hi", true},
		{"hey", true},
		{"hello", true},
		{"yo", true},
		{"sup", true},
		{"what's up", true},
		{"whats up", true},
		{"howdy", true},
		{"hola", true},
		{"hi there", true},
		{"hey!", true},
		{"hello, how are you", true},
		{"I need a 2 bedroom", false},
		{"what's the rent?", false},
		{"can I schedule a tour?", false},
		{"hi, I'm looking for a 1 bedroom", true}, // starts with greeting
	}

	for _, tt := range tests {
		result := isGreeting(tt.input)
		if result != tt.expected {
			t.Errorf("isGreeting(%q) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestQuestionAppearsInText(t *testing.T) {
	tests := []struct {
		question string
		text     string
		expected bool
	}{
		{"What is your monthly budget?", "my budget is around $1200", true},
		{"What is your monthly budget?", "I don't know", false},
		{"How many people will be living in the apartment?", "3 adults will be living there", false}, // only 1/3 keywords match
		{"Do you have any animals?", "I have a dog", false}, // "animals" not in answer
		{"Do you have any animals?", "no animals, just me", true},
		{"When are you looking to move in?", "sometime in august", false}, // "looking" not in answer
	}

	for _, tt := range tests {
		result := questionAppearsInText(tt.question, tt.text)
		if result != tt.expected {
			t.Errorf("questionAppearsInText(%q, %q) = %v, want %v", tt.question, tt.text, result, tt.expected)
		}
	}
}

func TestParserOutput_Struct(t *testing.T) {
	// Verify the ParserOutput struct JSON tags match expected field names
	out := ParserOutput{
		Budget:            "$1200",
		MoveInDate:        "August 1",
		JobTitle:          "nurse",
		Industry:          "healthcare",
		Pets:              "1 dog",
		BedroomPreference: "1 Bedroom",
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("failed to marshal ParserOutput: %v", err)
	}
	var decoded ParserOutput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ParserOutput: %v", err)
	}
	if decoded.Budget != "$1200" {
		t.Errorf("Budget = %q, want %q", decoded.Budget, "$1200")
	}
	if decoded.Pets != "1 dog" {
		t.Errorf("Pets = %q, want %q", decoded.Pets, "1 dog")
	}
	if decoded.BedroomPreference != "1 Bedroom" {
		t.Errorf("BedroomPreference = %q, want %q", decoded.BedroomPreference, "1 Bedroom")
	}
}

func TestParserOutput_EmptyJSON(t *testing.T) {
	// Verify unmarshaling {} produces empty ParserOutput (all fields zero)
	var decoded ParserOutput
	if err := json.Unmarshal([]byte(`{}`), &decoded); err != nil {
		t.Fatalf("failed to unmarshal empty JSON: %v", err)
	}
	if decoded.Budget != "" || decoded.MoveInDate != "" || decoded.Pets != "" {
		t.Errorf("empty ParserOutput should have all empty fields, got %+v", decoded)
	}
}

func TestSeedQualificationQuestions_OnlyQuestionsField(t *testing.T) {
	lead := &types.Lead{}
	cmdCenter := bson.M{
		"questions":  "What is your budget?\nWhen are you looking to move?",
		"highlights": "Fully furnished\nClose to campus",
		"keyInfo":    "Individual leases\nNo hidden fees",
		"priorities": "Get them to tour within 48 hours",
	}

	modified := seedQualificationQuestions(lead, cmdCenter)
	if !modified {
		t.Error("expected modified=true")
	}

	// Only "questions" field seeds — expect 2 questions, NOT 7
	if len(lead.QualificationQuestions) != 2 {
		t.Fatalf("expected 2 questions (only from 'questions' field), got %d", len(lead.QualificationQuestions))
	}

	// Check categories — all should be "qualifications"
	categories := map[string]int{}
	for _, q := range lead.QualificationQuestions {
		categories[q.Category]++
	}

	if categories["qualifications"] != 2 {
		t.Errorf("expected 2 qualifications, got %d", categories["qualifications"])
	}
	if categories["highlights"] != 0 {
		t.Errorf("highlights should NOT be seeded, got %d", categories["highlights"])
	}
	if categories["keyInfo"] != 0 {
		t.Errorf("keyInfo should NOT be seeded, got %d", categories["keyInfo"])
	}
	if categories["priorities"] != 0 {
		t.Errorf("priorities should NOT be seeded, got %d", categories["priorities"])
	}
}

func TestSeedQualificationQuestions_Dedup(t *testing.T) {
	existing := time.Now()
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "existing", Category: "qualifications", Question: "What is your budget?", AskedAt: &existing, AnsweredAt: &existing, Answer: "$1200"},
		},
	}
	cmdCenter := bson.M{
		"questions": "What is your budget?\nWhen are you looking to move?",
	}

	modified := seedQualificationQuestions(lead, cmdCenter)
	if !modified {
		t.Error("expected modified=true for new question")
	}

	// Should keep existing answered question + add new one
	if len(lead.QualificationQuestions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(lead.QualificationQuestions))
	}

	// Existing answered question should still be answered
	if lead.QualificationQuestions[0].Answer != "$1200" {
		t.Errorf("existing answer lost, got %q", lead.QualificationQuestions[0].Answer)
	}
}

func TestBuildQualificationContext_Structured(t *testing.T) {
	now := time.Now()
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q1", Question: "Budget?", AskedAt: &now, AnsweredAt: &now, Answer: "$1200"},
			{ID: "q2", Question: "Move-in date?", AskedAt: &now},
			{ID: "q3", Question: "Pets?"},
		},
	}
	cmdCenter := bson.M{"qualificationMode": "structured"}

	ctx := buildQualificationContext(lead, cmdCenter)
	if ctx.Mode != "structured" {
		t.Errorf("expected mode 'structured', got %q", ctx.Mode)
	}
	if len(ctx.Questions) != 3 {
		t.Fatalf("expected 3 questions, got %d", len(ctx.Questions))
	}
	if ctx.Questions[0].Status != "answered" {
		t.Errorf("q1 expected 'answered', got %q", ctx.Questions[0].Status)
	}
	if ctx.Questions[1].Status != "unanswered" {
		t.Errorf("q2 expected 'unanswered', got %q", ctx.Questions[1].Status)
	}
	if ctx.Questions[2].Status != "not-asked" {
		t.Errorf("q3 expected 'not-asked', got %q", ctx.Questions[2].Status)
	}
}

func TestBuildQualificationContext_FreeText(t *testing.T) {
	lead := &types.Lead{}
	cmdCenter := bson.M{"qualificationMode": "free-text"}

	ctx := buildQualificationContext(lead, cmdCenter)
	if ctx.Mode != "free-text" {
		t.Errorf("expected mode 'free-text', got %q", ctx.Mode)
	}
}

func TestNormalizeQuestion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"What is your budget?", "what is your budget"},
		{"  How many bedrooms?  ", "how many bedrooms"},
		{"When are you looking to move in?", "when are you looking to move in"},
	}

	for _, tt := range tests {
		result := normalizeQuestion(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeQuestion(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
