package generator

import (
	"strings"
	"testing"
	"time"

	"bamboo/types"
	"go.mongodb.org/mongo-driver/bson"
)

// TestPromptBuilder_LeadContext_AllFieldsRendered verifies the new
// lead-context fields (email, phone, source, industry, tags, comments)
// are rendered in the LEAD_CONTEXT section.
func TestPromptBuilder_LeadContext_AllFieldsRendered(t *testing.T) {
	cfg := PromptConfig{
		LeadFirstName:       "Haley",
		LeadLastName:        "Davis",
		LeadEmail:           "haleyannjack1995@yahoo.com",
		LeadPhone:           "9032674707",
		LeadStatus:          "Nurture",
		LeadSource:          "Zillow",
		LeadTags:            []string{"student", "3bed"},
		LeadBudget:          "$800-1000",
		LeadMoveIn:          "monday 16th june",
		LeadJobTitle:        "Nurse",
		LeadIndustry:        "Healthcare",
		LeadPets:            "3 pets",
		LeadBedroomPreference: "1 bed",
		LeadComments:        []string{"VIP", "follow up wednesday"},
	}
	out := BuildSystemPrompt(cfg)

	expected := []string{
		"Email: haleyannjack1995@yahoo.com",
		"Phone: 9032674707",
		"Status: Nurture",
		"Source: Zillow",
		"Tags: [student, 3bed]",
		"Budget: $800-1000",
		"Move-In: monday 16th june",
		"Job Title: Nurse",
		"Industry: Healthcare",
		"Pets: 3 pets",
		"Bedroom Pref: 1 bed",
		"Comments: VIP; follow up wednesday",
	}
	for _, s := range expected {
		if !strings.Contains(out, s) {
			t.Errorf("expected %q in LEAD_CONTEXT, got:\n%s", s, out)
		}
	}
}

// TestPromptBuilder_LeadContext_EmptyLeadsToNoSection verifies the
// LEAD_CONTEXT section is skipped entirely when no lead info is set.
func TestPromptBuilder_LeadContext_EmptyLeadsToNoSection(t *testing.T) {
	cfg := PromptConfig{} // no lead info
	out := BuildSystemPrompt(cfg)
	// "1. LEAD_CONTEXT" appears in the importance order list (always);
	// the actual section header is what we check.
	if strings.Contains(out, "═════════════\n1. LEAD_CONTEXT") {
		t.Errorf("expected NO LEAD_CONTEXT section when empty, got:\n%s", out)
	}
}

// TestPromptBuilder_PrivateNotes_RenderedAsPrivate verifies the
// PRIVATE TEAM NOTES section is rendered with the explicit
// "NEVER EXPOSE" header when notes exist.
func TestPromptBuilder_PrivateNotes_RenderedAsPrivate(t *testing.T) {
	cfg := PromptConfig{
		LeadNotes: []types.LeadNote{
			{
				ID:         "n1",
				TeamID:     "t1",
				LeadID:     "l1",
				AuthorName: "Support Bamboo",
				Content:    "HELLO THIS IS PRIVATE",
				CreatedAt:  time.Date(2026, 6, 16, 21, 28, 3, 0, time.UTC),
			},
		},
	}
	out := BuildSystemPrompt(cfg)

	if !strings.Contains(out, "PRIVATE TEAM NOTES") {
		t.Errorf("expected PRIVATE TEAM NOTES header, got:\n%s", out)
	}
	if !strings.Contains(out, "NEVER EXPOSE TO LEAD") {
		t.Errorf("expected 'NEVER EXPOSE TO LEAD' warning, got:\n%s", out)
	}
	if !strings.Contains(out, "Support Bamboo") {
		t.Errorf("expected author name, got:\n%s", out)
	}
	if !strings.Contains(out, "HELLO THIS IS PRIVATE") {
		t.Errorf("expected note content, got:\n%s", out)
	}
	if !strings.Contains(out, "2026-06-16") {
		t.Errorf("expected note timestamp, got:\n%s", out)
	}
}

// TestPromptBuilder_PrivateNotes_EmptyLeadsToNoSection verifies the
// PRIVATE TEAM NOTES section is skipped when no notes are set.
func TestPromptBuilder_PrivateNotes_EmptyLeadsToNoSection(t *testing.T) {
	cfg := PromptConfig{} // no notes
	out := BuildSystemPrompt(cfg)
	// The actual section header (with delimiter) is what we check.
	// The STRICT_RULE 9 mentions "PRIVATE TEAM NOTES" as an example,
	// which is expected.
	if strings.Contains(out, "═════════════\n1.25. PRIVATE TEAM NOTES") {
		t.Errorf("expected NO PRIVATE TEAM NOTES section when empty, got:\n%s", out)
	}
}

// TestPromptBuilder_StrictRule_PrivateNeverExpose verifies rule 9 in
// STRICT_RULES explicitly mentions PRIVATE sections and the never-expose
// rule.
func TestPromptBuilder_StrictRule_PrivateNeverExpose(t *testing.T) {
	cfg := PromptConfig{} // basic prompt
	out := BuildSystemPrompt(cfg)

	if !strings.Contains(out, "9. PRIVATE sections") {
		t.Errorf("expected rule 9 about PRIVATE sections, got:\n%s", out)
	}
	if !strings.Contains(out, "NEVER quote, paraphrase, summarize, or hint") {
		t.Errorf("expected explicit 'NEVER quote, paraphrase' wording, got:\n%s", out)
	}
}

// TestPromptBuilder_Timezone_DefaultAndOverride verifies CURRENT_DATE
// and CURRENT_TIME render when set, in the team's timezone.
func TestPromptBuilder_Timezone_RendersInMiscellaneous(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("America/Chicago tz not available: %v", err)
	}
	cfg := PromptConfig{
		CurrentDateTime: time.Date(2026, 6, 16, 14, 45, 0, 0, loc),
		TeamTimezone:    "America/Chicago",
		TeamName:        "Bamboo",
	}
	out := BuildSystemPrompt(cfg)

	if !strings.Contains(out, "CURRENT_DATE: 2026-06-16") {
		t.Errorf("expected CURRENT_DATE: 2026-06-16, got:\n%s", out)
	}
	if !strings.Contains(out, "TEAM_TIMEZONE: America/Chicago") {
		t.Errorf("expected TEAM_TIMEZONE line, got:\n%s", out)
	}
	if !strings.Contains(out, "CURRENT_TIME:") {
		t.Errorf("expected CURRENT_TIME line, got:\n%s", out)
	}
}

// TestPromptBuilder_Timezone_NotRenderedWhenEmpty verifies the
// MISCELLANEOUS section is NOT rendered when no team info AND no
// date/time info is set.
func TestPromptBuilder_Timezone_NotRenderedWhenEmpty(t *testing.T) {
	cfg := PromptConfig{} // nothing
	out := BuildSystemPrompt(cfg)
	// The "10. MISCELLANEOUS" in the importance order list is always
	// shown. Check for the actual section header (with delimiter).
	if strings.Contains(out, "═════════════\n10. MISCELLANEOUS\n═════════════") {
		t.Errorf("expected NO MISCELLANEOUS section when nothing to render, got:\n%s", out)
	}
	if strings.Contains(out, "CURRENT_DATE") {
		t.Errorf("expected NO CURRENT_DATE when not set, got:\n%s", out)
	}
	if strings.Contains(out, "TEAM_TIMEZONE") {
		t.Errorf("expected NO TEAM_TIMEZONE when not set, got:\n%s", out)
	}
}

// TestPromptBuilder_Timezone_InvalidTimezoneFallsBackToUTC verifies
// that an unknown timezone string doesn't crash the prompt builder
// and falls back to UTC.
func TestPromptBuilder_Timezone_InvalidTimezoneFallsBackToUTC(t *testing.T) {
	cfg := PromptConfig{
		CurrentDateTime: time.Date(2026, 6, 16, 14, 45, 0, 0, time.UTC),
		TeamTimezone:    "Not/A/Real/Timezone",
		TeamName:        "Bamboo",
	}
	out := BuildSystemPrompt(cfg)
	// Should not panic and should still render date/time.
	if !strings.Contains(out, "CURRENT_DATE: 2026-06-16") {
		t.Errorf("expected CURRENT_DATE despite invalid timezone, got:\n%s", out)
	}
}

// TestFormatTourLine_WithPropertyAndUnit verifies the new tour line
// format includes both property and unit names.
func TestFormatTourLine_WithPropertyAndUnit(t *testing.T) {
	m := bsonMapMember("potmanager", "potmanager@example.com")
	startTime := time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)

	got := formatTourLine(m, startTime, "Reata at Alamo Ranch", "Unit 8209", "UTC")
	want := "- Wed Jun 17, 2:00 PM UTC at Reata at Alamo Ranch (Unit 8209) with potmanager"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormatTourLine_PropertyOnly verifies the tour line degrades
// gracefully when only the property name is available.
func TestFormatTourLine_PropertyOnly(t *testing.T) {
	m := bsonMapMember("potmanager", "")
	startTime := time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)

	got := formatTourLine(m, startTime, "Reata at Alamo Ranch", "", "UTC")
	want := "- Wed Jun 17, 2:00 PM UTC at Reata at Alamo Ranch with potmanager"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormatTourLine_NoNames verifies the tour line still works
// when no property/unit names are resolved (legacy behavior).
func TestFormatTourLine_NoNames(t *testing.T) {
	m := bsonMapMember("potmanager", "")
	startTime := time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)

	got := formatTourLine(m, startTime, "", "", "UTC")
	want := "- Wed Jun 17, 2:00 PM UTC with potmanager"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormatTourLine_WithStatus verifies that previous tours get
// their status suffix (e.g. [cancelled], [no-show]).
func TestFormatTourLine_WithStatus(t *testing.T) {
	m := bsonMapMember("potmanager", "")
	m["status"] = "cancelled"
	startTime := time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)

	got := formatTourLine(m, startTime, "Reata at Alamo Ranch", "Unit 8209", "UTC")
	want := "- Wed Jun 17, 2:00 PM UTC at Reata at Alamo Ranch (Unit 8209) with potmanager [cancelled]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// bsonMapMember is a test helper for building meeting bson.M values.
func bsonMapMember(name, email string) bson.M {
	return bson.M{
		"member": bson.M{
			"name":  name,
			"email": email,
		},
	}
}
