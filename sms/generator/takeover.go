package generator

import (
	"bamboo/sms/core"
	"strings"
)

// detectHumanTakeover returns true if the most recent outbound message
// in the chat history was sent by a human agent (SentBy != ""). Used to
// inject the HUMAN TAKEOVER section at the top of the system prompt so
// the AI knows to match the human's tone and not contradict them.
//
// Walks the chat slice from newest to oldest. The first outbound message
// (Direction == "outbound") determines the answer — anything before it
// is irrelevant. If no outbound messages exist, returns false.
func detectHumanTakeover(chat []core.SMSMessage) bool {
	for i := len(chat) - 1; i >= 0; i-- {
		if chat[i].Direction == "outbound" {
			return chat[i].SentBy != ""
		}
	}
	return false
}

// detectHumanTakeoverFromThread inspects the rendered chat thread
// (the [Turn N] formatted string) and returns true if the most recent
// "AI:" or "Team:" line is a "Team:" line. This is a string-based
// fallback for callers that only have the rendered thread (not the
// raw message slice).
func detectHumanTakeoverFromThread(thread string) bool {
	lines := strings.Split(thread, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "Team:") {
			return true
		}
		if strings.HasPrefix(l, "AI:") {
			return false
		}
		if strings.HasPrefix(l, "Lead:") {
			continue // skip lead lines, keep looking
		}
	}
	return false
}

