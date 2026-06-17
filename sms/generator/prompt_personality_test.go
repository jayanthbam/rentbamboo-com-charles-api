package generator

import (
	"strings"
	"testing"
)

// TestPromptBuilder_Personality_AllValues covers all 5 Zod enum values
// (consultant, challenger, relationship, expert, closer) plus the
// empty/default case. The PERSONALITY section must:
//   - Render the "0.5. PERSONALITY" header
//   - Use the friendly label in the prompt
//   - Render tone-specific guidance
func TestPromptBuilder_Personality_AllValues(t *testing.T) {
	cases := []struct {
		enum   string
		label  string
		marker string // unique phrase expected in the rendered text
	}{
		{"relationship", "Relationship Builder", "build rapport first"},
		{"consultant", "Consultative", "diagnostic questions"},
		{"challenger", "Challenger", "push back on objections"},
		{"expert", "Expert", "data-driven"},
		{"closer", "Closer", "create urgency"},
	}
	for _, c := range cases {
		cfg := PromptConfig{Personality: c.enum}
		out := BuildSystemPrompt(cfg)

		if !strings.Contains(out, "0.5. PERSONALITY") {
			t.Errorf("[%s] expected '0.5. PERSONALITY' header, got:\n%s", c.enum, out)
		}
		if !strings.Contains(out, "your texting style") {
			t.Errorf("[%s] expected 'your texting style' subtitle, got:\n%s", c.enum, out)
		}
		if !strings.Contains(out, c.label+" style:") {
			t.Errorf("[%s] expected '%s style:' label, got:\n%s", c.enum, c.label, out)
		}
		if !strings.Contains(out, c.marker) {
			t.Errorf("[%s] expected tone marker '%s', got:\n%s", c.enum, c.marker, out)
		}
	}
}

// TestPromptBuilder_Personality_Empty verifies the section is NOT
// rendered when the personality field is empty (e.g. older command
// centers without a personality set). The header still appears in the
// importance order list (always), but the actual style guide section
// must not render.
func TestPromptBuilder_Personality_Empty(t *testing.T) {
	cfg := PromptConfig{} // no personality
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "your texting style") {
		t.Errorf("expected NO PERSONALITY section when empty, got:\n%s", out)
	}
	if strings.Contains(out, "style:") {
		t.Errorf("expected NO '<Label> style:' when empty, got:\n%s", out)
	}
}

// TestPromptBuilder_Personality_UnknownFallsBackToDefault verifies an
// unknown enum value (e.g. a future personality the backend doesn't
// know about) falls back to a sensible default and still renders.
func TestPromptBuilder_Personality_UnknownFallsBackToDefault(t *testing.T) {
	cfg := PromptConfig{Personality: "future-personality-type"}
	out := BuildSystemPrompt(cfg)
	if !strings.Contains(out, "0.5. PERSONALITY") {
		t.Errorf("expected PERSONALITY section to render for unknown value, got:\n%s", out)
	}
	// Default wording should be present.
	if !strings.Contains(out, "professional, warm, and direct") {
		t.Errorf("expected default 'professional, warm, and direct' wording, got:\n%s", out)
	}
}

// TestPromptBuilder_Personality_InImportanceOrder verifies PERSONALITY
// is listed in the importance order list between STRICT_RULES (0) and
// LEAD_CONTEXT (1) as 0.5.
func TestPromptBuilder_Personality_InImportanceOrder(t *testing.T) {
	cfg := PromptConfig{Personality: "relationship"}
	out := BuildSystemPrompt(cfg)

	// Find the importance order section.
	idx := strings.Index(out, "Importance order")
	if idx == -1 {
		t.Fatalf("expected 'Importance order' section, got:\n%s", out)
	}
	// Slice to the next blank line.
	section := out[idx:]
	if eol := strings.Index(section, "\n\n"); eol != -1 {
		section = section[:eol]
	}
	if !strings.Contains(section, "0.5. PERSONALITY") {
		t.Errorf("expected '0.5. PERSONALITY' in importance order:\n%s", section)
	}
	// 0.5 must come after 0. STRICT_RULES and before 1. LEAD_CONTEXT.
	strictIdx := strings.Index(section, "0. STRICT_RULES")
	personalityIdx := strings.Index(section, "0.5. PERSONALITY")
	leadIdx := strings.Index(section, "1. LEAD_CONTEXT")
	if !(strictIdx < personalityIdx && personalityIdx < leadIdx) {
		t.Errorf("importance order wrong: strict=%d personality=%d lead=%d\n%s", strictIdx, personalityIdx, leadIdx, section)
	}
}

// TestPromptBuilder_Personality_PositionedAfterFullyQualified verifies
// the PERSONALITY section appears AFTER the FULLY QUALIFIED marker but
// BEFORE STRICT_RULES.
func TestPromptBuilder_Personality_PositionedAfterFullyQualified(t *testing.T) {
	cfg := PromptConfig{
		Personality:      "relationship",
		IsFullyQualified: true,
	}
	out := BuildSystemPrompt(cfg)

	// Find the LAST occurrence of each marker (the importance order list
	// also mentions these names; we want the actual section).
	fqIdx := strings.LastIndex(out, "✓ LEAD IS FULLY QUALIFIED")
	perIdx := strings.LastIndex(out, "0.5. PERSONALITY")
	strictIdx := strings.LastIndex(out, "0. STRICT_RULES")

	if fqIdx == -1 {
		t.Fatalf("expected FULLY QUALIFIED section, got:\n%s", out)
	}
	if perIdx == -1 {
		t.Fatalf("expected PERSONALITY section, got:\n%s", out)
	}
	if strictIdx == -1 {
		t.Fatalf("expected STRICT_RULES section, got:\n%s", out)
	}
	if !(fqIdx < perIdx && perIdx < strictIdx) {
		t.Errorf("expected order: FULLY QUALIFIED < PERSONALITY < STRICT_RULES, got fq=%d per=%d strict=%d", fqIdx, perIdx, strictIdx)
	}
}

// TestPersonalityStyle_DirectHelper covers the personalityStyle()
// helper in isolation. This makes the mapping behavior easy to verify
// without re-rendering the full prompt.
func TestPersonalityStyle_DirectHelper(t *testing.T) {
	cases := map[string]string{
		"relationship": "Relationship Builder",
		"consultant":   "Consultative",
		"challenger":   "Challenger",
		"expert":       "Expert",
		"closer":       "Closer",
	}
	for enum, label := range cases {
		style, ok := personalityStyle(enum)
		if !ok {
			t.Errorf("[%s] expected ok=true, got false", enum)
		}
		if !strings.Contains(style, label+" style:") {
			t.Errorf("[%s] expected label '%s style:' in style text, got: %s", enum, label, style)
		}
	}
}
