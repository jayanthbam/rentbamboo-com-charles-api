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

// TestTourPushRule11_TourOnly verifies the "tour only" version renders
// when only TourScheduling is on (no app sending).
func TestTourPushRule11_TourOnly(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: true,
		ScheduleURL:    "https://example.com/schedule/abc",
		AppSending:     false,
	}
	out := BuildSystemPrompt(cfg)
	if !strings.Contains(out, "11. Don't spam the tour link") {
		t.Errorf("expected '11. Don't spam the tour link', got:\n%s", out)
	}
	if !strings.Contains(out, "TOUR_LINK") {
		t.Errorf("expected TOUR_LINK reference, got:\n%s", out)
	}
	if strings.Contains(out, "application link") {
		t.Errorf("expected NO application link mention (app not enabled), got:\n%s", out)
	}
}

// TestTourPushRule11_AppOnly verifies the "app only" version renders
// when only AppSending is on (no tour scheduling).
func TestTourPushRule11_AppOnly(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: false,
		AppSending:     true,
		AppURL:         "https://example.com/apply/abc",
	}
	out := BuildSystemPrompt(cfg)
	if !strings.Contains(out, "11. Don't spam the application link") {
		t.Errorf("expected '11. Don't spam the application link', got:\n%s", out)
	}
	if !strings.Contains(out, "APPLICATION_LINK") {
		t.Errorf("expected APPLICATION_LINK reference, got:\n%s", out)
	}
	if strings.Contains(out, "TOUR_LINK") {
		t.Errorf("expected NO TOUR_LINK mention (tour not enabled), got:\n%s", out)
	}
}

// TestTourPushRule11_BothOn verifies the combined version renders when
// both tour and app sending are on.
func TestTourPushRule11_BothOn(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: true,
		ScheduleURL:    "https://example.com/schedule/abc",
		AppSending:     true,
		AppURL:         "https://example.com/apply/abc",
	}
	out := BuildSystemPrompt(cfg)
	if !strings.Contains(out, "11. Don't spam the tour or application links") {
		t.Errorf("expected '11. Don't spam the tour or application links', got:\n%s", out)
	}
	if !strings.Contains(out, "TOUR_LINK") {
		t.Errorf("expected TOUR_LINK in both version, got:\n%s", out)
	}
	if !strings.Contains(out, "APPLICATION_LINK") {
		t.Errorf("expected APPLICATION_LINK in both version, got:\n%s", out)
	}
}

// TestTourPushRule11_BothOff verifies rule 11 does NOT render when
// neither tour nor app sending is enabled.
func TestTourPushRule11_BothOff(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: false,
		AppSending:     false,
	}
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "11. Don't spam") {
		t.Errorf("expected NO rule 11 when both capabilities off, got:\n%s", out)
	}
	if strings.Contains(out, "10. If the lead asks for a photo") {
		t.Errorf("expected NO rule 10 when tour off (no tour link), got:\n%s", out)
	}
}

// TestTourPushRule11_AbsentWhenNoScheduleURL verifies rule 11 doesn't
// render for the tour path when ScheduleURL is empty.
func TestTourPushRule11_AbsentWhenNoScheduleURL(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: true,
		ScheduleURL:    "",
	}
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "11. Don't spam") {
		t.Errorf("expected NO rule 11 when no schedule URL, got:\n%s", out)
	}
}

// TestTourPushRule11_AbsentWhenNoAppURL verifies rule 11 doesn't render
// for the app path when AppURL is empty.
func TestTourPushRule11_AbsentWhenNoAppURL(t *testing.T) {
	cfg := PromptConfig{
		AppSending: true,
		AppURL:     "",
	}
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "11. Don't spam") {
		t.Errorf("expected NO rule 11 when no app URL, got:\n%s", out)
	}
}

// TestTourPushRule11_JobsDone verifies rule 11 is NOT rendered when
// the lead is fully qualified AND has a tour scheduled ("jobs done" —
// the AI is in "be of property help" mode).
func TestTourPushRule11_JobsDone(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling:    true,
		ScheduleURL:       "https://example.com/schedule/abc",
		AppSending:        true,
		AppURL:            "https://example.com/apply/abc",
		IsFullyQualified:  true,
		ToursScheduled:    "- Wed Jun 17, 2:00 PM CDT at Reata with Jay",
	}
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "11. Don't spam") {
		t.Errorf("expected NO rule 11 when jobs done (qualified + tour scheduled), got:\n%s", out)
	}
	// Rule 10 should still render (photo/video redirect is still useful).
	if !strings.Contains(out, "10. If the lead asks for a photo") {
		t.Errorf("expected rule 10 to still render even when jobs done, got:\n%s", out)
	}
}

// TestTourPushRule11_JobsDone_TourOnly verifies the jobs-done skip
// applies to the tour-only version too (not just both).
func TestTourPushRule11_JobsDone_TourOnly(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling:   true,
		ScheduleURL:      "https://example.com/schedule/abc",
		AppSending:       false,
		IsFullyQualified: true,
		ToursScheduled:   "- Wed Jun 17, 2:00 PM CDT at Reata with Jay",
	}
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "11. Don't spam") {
		t.Errorf("expected NO rule 11 when jobs done (tour only), got:\n%s", out)
	}
}

// TestTourPushRule11_TourBookedSubClause verifies the "tour booked" sub-
// clause renders when ToursScheduled is set but the lead is NOT fully
// qualified. The lead has a tour but the AI should still not re-push
// the tour link.
func TestTourPushRule11_TourBookedSubClause(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: true,
		ScheduleURL:    "https://example.com/schedule/abc",
		ToursScheduled: "- Wed Jun 17, 2:00 PM CDT at Reata with Jay",
		// IsFullyQualified is false (default) — lead has a tour but
		// not all questions answered, so rule 11 should still render
		// with the "tour booked" sub-clause.
	}
	out := BuildSystemPrompt(cfg)
	if !strings.Contains(out, "11. Don't spam the tour link") {
		t.Errorf("expected rule 11 to render when tour booked but not qualified, got:\n%s", out)
	}
	if !strings.Contains(out, "After the lead has already been sent the tour link") {
		t.Errorf("expected 'tour booked' sub-clause, got:\n%s", out)
	}
	if !strings.Contains(out, "or has a tour scheduled") {
		t.Errorf("expected 'has a tour scheduled' in sub-clause, got:\n%s", out)
	}
}

// TestTourPushRule11_AppSubmittedSubClause verifies the "app submitted"
// sub-clause renders when LeadStatus is "Application".
func TestTourPushRule11_AppSubmittedSubClause(t *testing.T) {
	cfg := PromptConfig{
		AppSending: true,
		AppURL:     "https://example.com/apply/abc",
		LeadStatus: "Application",
	}
	out := BuildSystemPrompt(cfg)
	if !strings.Contains(out, "11. Don't spam the application link") {
		t.Errorf("expected rule 11 to render for app only, got:\n%s", out)
	}
	if !strings.Contains(out, "After the lead has already applied") {
		t.Errorf("expected 'app submitted' sub-clause, got:\n%s", out)
	}
	if !strings.Contains(out, "Status: Application") {
		t.Errorf("expected 'Status: Application' reference, got:\n%s", out)
	}
}

// TestTourPushRule11_BothOn_BothSubClauses verifies the combined version
// includes BOTH the tour-booked and app-submitted sub-clauses.
func TestTourPushRule11_BothOn_BothSubClauses(t *testing.T) {
	cfg := PromptConfig{
		TourScheduling: true,
		ScheduleURL:    "https://example.com/schedule/abc",
		AppSending:     true,
		AppURL:         "https://example.com/apply/abc",
		LeadStatus:     "Application",
		ToursScheduled: "- Wed Jun 17, 2:00 PM CDT at Reata with Jay",
		// IsFullyQualified must be false for the rule to render
		// (jobs-done check requires IsFullyQualified=true).
	}
	out := BuildSystemPrompt(cfg)
	if !strings.Contains(out, "11. Don't spam the tour or application links") {
		t.Errorf("expected both version, got:\n%s", out)
	}
	if !strings.Contains(out, "After the lead has already been sent the tour link") {
		t.Errorf("expected tour-booked sub-clause, got:\n%s", out)
	}
	if !strings.Contains(out, "After the lead has already applied") {
		t.Errorf("expected app-submitted sub-clause, got:\n%s", out)
	}
}

// TestTourPushRule11_CoexistsWithRule10 verifies rule 10 (photo/video)
// renders alongside rule 11 when tour is on with a URL.
func TestTourPushRule11_CoexistsWithRule10(t *testing.T) {
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
