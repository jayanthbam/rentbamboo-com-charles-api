package generator

// QualificationPromptItem represents a single qualification question and its state.
// Used by the prompt builder to render structured qualification context.
type QualificationPromptItem struct {
	Question string // The question text
	Status   string // "answered", "unanswered", "not-asked"
	Answer   string // The lead's answer text (empty if not yet answered)
}

// QualificationContext holds the structured qualification state for the prompt builder.
// When Mode is "structured", Questions contains the tracked Q&A state from the lead document.
// When Mode is "free-text" or empty, the prompt builder falls back to the legacy CompletedItems/QualStatus strings.
type QualificationContext struct {
	Mode      string                     // "free-text" (default) or "structured"
	Questions []QualificationPromptItem  // Ordered list of questions with their status
}
