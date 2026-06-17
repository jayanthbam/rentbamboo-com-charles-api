package helpers

import (
	"fmt"
	"regexp"
	"strings"
)

// Hours-related regex patterns to detect when a user is asking about
// office hours, business hours, leasing hours, or general availability.
var (
	// Direct hours phrases: "office hours", "business hours", "leasing hours",
	// "your hours", "hours of operation", "the hours"
	// Requires a qualifier prefix (office/business/leasing/your/the) OR the suffix
	// "of operation" to avoid matching duration phrases like "2 hours".
	hoursPhrasesRe = regexp.MustCompile(`(?i)\b(?:(?:office|business|leasing|leasing\s+office|your|the)\s+hours|hours\s+of\s+operation)\b`)

	// "when are you open", "are you open today", "are you open right now",
	// "are you open on saturday"
	hoursOpenRe = regexp.MustCompile(`(?i)\b(?:are\s+you|is\s+(?:the\s+)?(?:office|leasing\s+office))\s+open\b`)

	// "when do you open", "when do you close", "what time do you open/close",
	// "when does the office open/close"
	hoursOpenCloseRe = regexp.MustCompile(`(?i)\b(?:when|what\s+time)\s+(?:do\s+you|does\s+(?:the\s+)?(?:office|leasing\s+office))\s+(?:open|close)\b`)

	// "what time can I come in", "when can I visit", "when can I come by",
	// "when can I stop by"
	hoursVisitRe = regexp.MustCompile(`(?i)\b(?:what\s+time|when)\s+can\s+I\s+(?:come\s+(?:in|by)|visit|stop\s+by|swing\s+by|drop\s+by)\b`)

	// "what days are you open", "which days are you open"
	hoursDaysRe = regexp.MustCompile(`(?i)\b(?:what|which)\s+days\s+(?:are\s+you|is\s+(?:the\s+)?(?:office|leasing\s+office))\s+open\b`)
)

// DetectHoursIntent returns true if the message is asking about office
// hours, business hours, or general time-of-day availability. This allows
// callers to short-circuit the AI response and redirect the user to the
// scheduling page (which shows real availability) rather than risking
// hallucinated hours.
func DetectHoursIntent(message string) bool {
	m := strings.TrimSpace(message)
	if m == "" {
		return false
	}

	if hoursPhrasesRe.MatchString(m) {
		return true
	}
	if hoursOpenRe.MatchString(m) {
		return true
	}
	if hoursOpenCloseRe.MatchString(m) {
		return true
	}
	if hoursVisitRe.MatchString(m) {
		return true
	}
	if hoursDaysRe.MatchString(m) {
		return true
	}

	return false
}

// HoursDeflectionResponse renders the text reply the AI should send when
// hours intent is detected. It encapsulates two branches:
//   - tourScheduling enabled AND a scheduling URL is available -> point the
//     user to the link where they can see real availability
//   - otherwise -> let the user know a team member will follow up
//
// The returned string is plain text, safe for SMS.
func HoursDeflectionResponse(tourSchedulingEnabled bool, scheduleURL string) string {
	scheduleURL = strings.TrimSpace(scheduleURL)

	if tourSchedulingEnabled && scheduleURL != "" {
		return fmt.Sprintf(
			"You can check our hours and availability here: %s",
			scheduleURL,
		)
	}

	return "Our hours may vary — a team member will reach out shortly to help you out!"
}
