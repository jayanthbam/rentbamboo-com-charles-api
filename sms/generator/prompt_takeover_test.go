package generator

import (
	"strings"
	"testing"
)

func TestPromptBuilder_HumanTakeover_AppearsWhenTrue(t *testing.T) {
	cfg := PromptConfig{
		HumanTakeover: true,
	}
	out := BuildSystemPrompt(cfg)
	if !strings.Contains(out, "🔴 HUMAN TAKEOVER — HIGH PRIORITY") {
		t.Errorf("expected HUMAN TAKEOVER section header, got:\n%s", out)
	}
	if !strings.Contains(out, "MUST:") {
		t.Errorf("expected MUST: instructions, got:\n%s", out)
	}
	if !strings.Contains(out, "**THIS PRECEDES OVER THE AI. **") {
		t.Errorf("expected the precedence note, got:\n%s", out)
	}
}

func TestPromptBuilder_HumanTakeover_AbsentWhenFalse(t *testing.T) {
	cfg := PromptConfig{
		HumanTakeover: false,
	}
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "HUMAN TAKEOVER") {
		t.Errorf("HUMAN TAKEOVER section should be absent when HumanTakeover=false, got:\n%s", out)
	}
}

func TestPromptBuilder_HumanTakeover_AbsentWhenZeroValue(t *testing.T) {
	cfg := PromptConfig{} // zero value — HumanTakeover defaults to false
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "HUMAN TAKEOVER") {
		t.Errorf("HUMAN TAKEOVER section should be absent on zero-value cfg, got:\n%s", out)
	}
}

func TestPromptBuilder_HumanTakeover_AppearsBeforeStrictRules(t *testing.T) {
	cfg := PromptConfig{
		HumanTakeover: true,
	}
	out := BuildSystemPrompt(cfg)
	takeoverIdx := strings.Index(out, "HUMAN TAKEOVER")
	// Find the ACTUAL STRICT_RULES section (the body, not the importance
	// list reference). The section starts with "═════════════\n0. STRICT_RULES".
	strictIdx := strings.Index(out, "═════════════\n0. STRICT_RULES")
	if takeoverIdx == -1 || strictIdx == -1 {
		t.Fatalf("expected both sections in output, takeover=%d strict=%d", takeoverIdx, strictIdx)
	}
	if takeoverIdx > strictIdx {
		t.Errorf("HUMAN TAKEOVER should appear BEFORE the STRICT_RULES body section, but takeover at %d > strict at %d",
			takeoverIdx, strictIdx)
	}
}
