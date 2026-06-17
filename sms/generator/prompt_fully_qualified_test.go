package generator

import (
	"strings"
	"testing"
)

// TestPromptBuilder_FullyQualified_WithTour verifies the "be of help"
// sub-condition when the lead is fully qualified AND a tour is
// already scheduled. The AI should:
//   - NOT push for a new tour
//   - Be of property help
//   - NOT re-ask answered questions
func TestPromptBuilder_FullyQualified_WithTour(t *testing.T) {
	cfg := PromptConfig{
		IsFullyQualified: true,
		ToursScheduled:   "- Wed Jun 17, 2:00 PM with potmanager",
	}
	out := BuildSystemPrompt(cfg)

	// Sub-case A marker should be present.
	if !strings.Contains(out, "✓ LEAD IS FULLY QUALIFIED (tour already scheduled)") {
		t.Errorf("expected sub-case A header, got:\n%s", out)
	}

	// Goal hierarchy should mention "BE OF HELP" mode.
	if !strings.Contains(out, "BE OF HELP") {
		t.Errorf("expected 'BE OF HELP' in goal hierarchy, got:\n%s", out)
	}

	// Should NOT push for a new tour.
	if !strings.Contains(out, "Do NOT push for a new tour") {
		t.Errorf("expected 'Do NOT push for a new tour' in FULLY QUALIFIED section, got:\n%s", out)
	}

	// Should explicitly mention property change nuance.
	if !strings.Contains(out, "Stale link is not property change") {
		t.Errorf("expected 'Stale link is not property change' nuance, got:\n%s", out)
	}

	// Should NOT render the "PUSH for a tour" goal (that's sub-case B).
	if strings.Contains(out, "PUSH for a tour (or close-out)") {
		t.Errorf("expected NOT to render PUSH-for-tour goal when tour is scheduled, got:\n%s", out)
	}

	// Importance line 6 should reflect the tour-scheduled sub-case.
	if !strings.Contains(out, "tour already scheduled, just be of help") {
		t.Errorf("expected sub-case A importance line 6, got:\n%s", out)
	}
}

// TestPromptBuilder_FullyQualified_WithoutTour verifies the "push for
// tour / close-out" sub-condition when the lead is fully qualified but
// no tour is scheduled. The AI should:
//   - Push for a tour
//   - Be concise
//   - NOT re-ask answered questions
func TestPromptBuilder_FullyQualified_WithoutTour(t *testing.T) {
	cfg := PromptConfig{
		IsFullyQualified: true,
		ToursScheduled:   "", // no scheduled tour
	}
	out := BuildSystemPrompt(cfg)

	// Sub-case B marker should be present.
	if !strings.Contains(out, "✓ LEAD IS FULLY QUALIFIED (no tour scheduled)") {
		t.Errorf("expected sub-case B header, got:\n%s", out)
	}

	// Goal hierarchy should mention "PUSH for a tour" mode.
	if !strings.Contains(out, "PUSH for a tour (or close-out)") {
		t.Errorf("expected 'PUSH for a tour (or close-out)' in goal hierarchy, got:\n%s", out)
	}

	// Should mention "Move to PUSH: tour scheduling" in FULLY QUALIFIED.
	if !strings.Contains(out, "Move to PUSH: tour scheduling") {
		t.Errorf("expected 'Move to PUSH: tour scheduling' in FULLY QUALIFIED section, got:\n%s", out)
	}

	// Should NOT render the "BE OF HELP" goal (that's sub-case A).
	if strings.Contains(out, "BE OF HELP") {
		t.Errorf("expected NOT to render BE-OF-HELP goal when no tour is scheduled, got:\n%s", out)
	}

	// Importance line 6 should reflect the no-tour sub-case.
	if !strings.Contains(out, "no unanswered questions — focus on PUSH for tour / close-out") {
		t.Errorf("expected sub-case B importance line 6, got:\n%s", out)
	}
}

// TestPromptBuilder_NotFullyQualified_DefaultWording verifies the
// default "qualify first" mode still renders when IsFullyQualified
// is false (no regression — the sub-conditions only apply when
// IsFullyQualified is true).
func TestPromptBuilder_NotFullyQualified_DefaultWording(t *testing.T) {
	cfg := PromptConfig{
		IsFullyQualified: false,
	}
	out := BuildSystemPrompt(cfg)

	// Default goal hierarchy should render.
	if !strings.Contains(out, "qualify first, push for a tour once qualified") {
		t.Errorf("expected default 'qualify first' goal hierarchy, got:\n%s", out)
	}

	// Default target should render.
	if !strings.Contains(out, "First qualify the lead.") {
		t.Errorf("expected default 'First qualify the lead' target, got:\n%s", out)
	}

	// Default importance line 6 should render.
	if !strings.Contains(out, "6. ASK_NEXT (unanswered — pick 1-2 to weave into your reply when needed)") {
		t.Errorf("expected default importance line 6, got:\n%s", out)
	}

	// FULLY QUALIFIED section should NOT render.
	if strings.Contains(out, "✓ LEAD IS FULLY QUALIFIED") {
		t.Errorf("FULLY QUALIFIED section should NOT render when IsFullyQualified=false, got:\n%s", out)
	}
}

// TestPromptBuilder_FullyQualified_BothSubConditions_NoStaleAsking
// is a higher-level integration test that verifies the user's nuance:
// "Do NOT re-ask any of the answered questions, unless you sense
// property change. Stale link is not property change."
func TestPromptBuilder_FullyQualified_BothSubConditions_NoStaleAsking(t *testing.T) {
	for _, subCase := range []struct {
		name            string
		toursScheduled  string
		expectedSubstr  string
	}{
		{
			name:           "with tour",
			toursScheduled: "- Wed Jun 17, 2:00 PM with potmanager",
			expectedSubstr: "tour already scheduled",
		},
		{
			name:           "without tour",
			toursScheduled: "",
			expectedSubstr: "no tour scheduled",
		},
	} {
		t.Run(subCase.name, func(t *testing.T) {
			cfg := PromptConfig{
				IsFullyQualified: true,
				ToursScheduled:   subCase.toursScheduled,
			}
			out := BuildSystemPrompt(cfg)

			// Both sub-cases should include the "Stale link" nuance.
			if !strings.Contains(out, "Stale link is not property change") {
				t.Errorf("[%s] expected 'Stale link is not property change' nuance, got:\n%s", subCase.name, out)
			}

			// Both sub-cases should NOT re-ask answered questions.
			if !strings.Contains(out, "Do NOT re-ask any of the answered questions") {
				t.Errorf("[%s] expected 'Do NOT re-ask' directive, got:\n%s", subCase.name, out)
			}

			// The sub-case identifier should be present in the FULLY QUALIFIED header.
			if !strings.Contains(out, subCase.expectedSubstr) {
				t.Errorf("[%s] expected sub-case identifier %q in header, got:\n%s", subCase.name, subCase.expectedSubstr, out)
			}
		})
	}
}
