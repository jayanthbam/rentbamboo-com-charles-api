package generator

import (
	"strings"
	"testing"
)

// TestPromptBuilder_TimingAwareness_RendersByDefault verifies the new
// timing-awareness sentence in the 3-sentence rules. It should always
// render (not conditional) so the AI knows how to interpret chat
// history timestamps.
func TestPromptBuilder_TimingAwareness_RendersByDefault(t *testing.T) {
	cfg := PromptConfig{} // no special config
	out := BuildSystemPrompt(cfg)
	if !strings.Contains(out, "Timestamps in the chat history show when each message was sent") {
		t.Errorf("expected timing-awareness sentence in opening, got:\n%s", out)
	}
	if !strings.Contains(out, "gauge recency") {
		t.Errorf("expected 'gauge recency' phrase, got:\n%s", out)
	}
}

// TestPromptBuilder_TourPushRule11_RendersWhenTourSchedulingOn verifies
// the new STRICT_RULE 11 renders when tour scheduling is on AND we have
// a schedule URL. Otherwise, rule 11 should NOT render.
func TestPromptBuilder_TourPushRule11_RendersWhenTourSchedulingOn(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: true,
		ScheduleURL:    "https://example.com/schedule/abc",
	}
	out := BuildSystemPrompt(cfg)
	if !strings.Contains(out, "11. Don't spam the tour link") {
		t.Errorf("expected STRICT_RULE 11 about not spamming tour link, got:\n%s", out)
	}
	if !strings.Contains(out, "once or twice across the whole conversation") {
		t.Errorf("expected 'once or twice' wording, got:\n%s", out)
	}
	if !strings.Contains(out, "TOUR HISTORY") {
		t.Errorf("expected reference to TOUR HISTORY in rule 11, got:\n%s", out)
	}
}

// TestPromptBuilder_TourPushRule11_AbsentWhenTourSchedulingOff verifies
// rule 11 is skipped when tour scheduling is OFF (matches the rule 10
// pattern).
func TestPromptBuilder_TourPushRule11_AbsentWhenTourSchedulingOff(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: false,
		ScheduleURL:    "https://example.com/schedule/abc",
	}
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "11. Don't spam the tour link") {
		t.Errorf("expected NO rule 11 when tour scheduling off, got:\n%s", out)
	}
}

// TestPromptBuilder_TourPushRule11_AbsentWhenNoScheduleURL verifies
// rule 11 is skipped when there's no schedule URL (matches rule 10).
func TestPromptBuilder_TourPushRule11_AbsentWhenNoScheduleURL(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: true,
		ScheduleURL:    "",
	}
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "11. Don't spam the tour link") {
		t.Errorf("expected NO rule 11 when no schedule URL, got:\n%s", out)
	}
}

// TestPromptBuilder_TourPushRule11_CoexistsWithRule10 verifies rule 11
// is rendered right after rule 10 (same conditional block).
func TestPromptBuilder_TourPushRule11_CoexistsWithRule10(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: true,
		ScheduleURL:    "https://example.com/schedule/abc",
	}
	out := BuildSystemPrompt(cfg)
	rule10Idx := strings.Index(out, "10. If the lead asks for a photo")
	rule11Idx := strings.Index(out, "11. Don't spam the tour link")
	if rule10Idx == -1 || rule11Idx == -1 {
		t.Fatalf("expected both rules 10 and 11, got 10=%d 11=%d", rule10Idx, rule11Idx)
	}
	if rule10Idx >= rule11Idx {
		t.Errorf("expected rule 10 before rule 11, got 10=%d 11=%d", rule10Idx, rule11Idx)
	}
}
