package generator

import (
	"testing"

	"bamboo/sms/core"
)

// ---- detectHumanTakeover (slice-based) ----

func TestDetectHumanTakeover_NoMessages(t *testing.T) {
	got := detectHumanTakeover(nil)
	if got {
		t.Errorf("expected false for empty chat, got true")
	}
}

func TestDetectHumanTakeover_OnlyLeadMessages(t *testing.T) {
	chat := []core.SMSMessage{
		{Direction: "inbound", Body: "hi"},
		{Direction: "inbound", Body: "2 bedrooms"},
	}
	got := detectHumanTakeover(chat)
	if got {
		t.Errorf("expected false for lead-only chat, got true")
	}
}

func TestDetectHumanTakeover_LastOutboundIsAI(t *testing.T) {
	chat := []core.SMSMessage{
		{Direction: "inbound", Body: "hi"},
		{Direction: "outbound", Body: "how many bedrooms?", SentBy: ""}, // AI
		{Direction: "inbound", Body: "2"},
	}
	got := detectHumanTakeover(chat)
	if got {
		t.Errorf("expected false when last outbound is AI, got true")
	}
}

func TestDetectHumanTakeover_LastOutboundIsHuman(t *testing.T) {
	chat := []core.SMSMessage{
		{Direction: "inbound", Body: "hi"},
		{Direction: "outbound", Body: "we have 2br available", SentBy: "user_abc123"},
		{Direction: "inbound", Body: "great"},
	}
	got := detectHumanTakeover(chat)
	if !got {
		t.Errorf("expected true when last outbound is human, got false")
	}
}

func TestDetectHumanTakeover_HumanAfterAI(t *testing.T) {
	// Even if AI sent earlier, the LAST outbound (the most recent one)
	// is what matters. If that's human, takeover is true.
	chat := []core.SMSMessage{
		{Direction: "outbound", Body: "ai message", SentBy: ""},
		{Direction: "inbound", Body: "lead reply"},
		{Direction: "outbound", Body: "human follow-up", SentBy: "user_xyz"},
	}
	got := detectHumanTakeover(chat)
	if !got {
		t.Errorf("expected true when last outbound is human (even if AI sent earlier), got false")
	}
}

// ---- detectHumanTakeoverFromThread (string-based) ----

func TestDetectHumanTakeoverFromThread_Empty(t *testing.T) {
	got := detectHumanTakeoverFromThread("")
	if got {
		t.Errorf("expected false for empty thread, got true")
	}
}

func TestDetectHumanTakeoverFromThread_OnlyLead(t *testing.T) {
	thread := "[Turn 1]\nLead: hi\nLead: 2 bedrooms\n"
	got := detectHumanTakeoverFromThread(thread)
	if got {
		t.Errorf("expected false for lead-only thread, got true")
	}
}

func TestDetectHumanTakeoverFromThread_LastLineIsAI(t *testing.T) {
	thread := "[Turn 1]\nLead: hi\nAI: how many bedrooms?\n"
	got := detectHumanTakeoverFromThread(thread)
	if got {
		t.Errorf("expected false when last line is AI:, got true")
	}
}

func TestDetectHumanTakeoverFromThread_LastLineIsTeam(t *testing.T) {
	thread := "[Turn 1]\nLead: hi\nAI: how many bedrooms?\n\n" +
		"[Turn 2]\nTeam: We have 2br available.\n"
	got := detectHumanTakeoverFromThread(thread)
	if !got {
		t.Errorf("expected true when last line is Team:, got false")
	}
}

func TestDetectHumanTakeoverFromThread_TrailingWhitespace(t *testing.T) {
	// Should handle trailing blank lines.
	thread := "[Turn 1]\nLead: hi\nTeam: hi back\n\n\n"
	got := detectHumanTakeoverFromThread(thread)
	if !got {
		t.Errorf("expected true despite trailing whitespace, got false")
	}
}

func TestDetectHumanTakeoverFromThread_TeamBeforeAI(t *testing.T) {
	// AI: appears AFTER Team: in the thread — AI is more recent, so
	// takeover is FALSE.
	thread := "[Turn 1]\nTeam: hi\nAI: how can I help?\n"
	got := detectHumanTakeoverFromThread(thread)
	if got {
		t.Errorf("expected false when AI is more recent than Team, got true")
	}
}
