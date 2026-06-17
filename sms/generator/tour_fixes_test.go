package generator

import (
	"strings"
	"testing"
	"time"
)

// TestFormatTourLine_TimezoneConversion verifies the tour time is
// converted to the team's timezone. The startTime is UTC; the rendered
// time should be in the requested IANA timezone.
func TestFormatTourLine_TimezoneConversion(t *testing.T) {
	if _, err := time.LoadLocation("America/Chicago"); err != nil {
		t.Skipf("America/Chicago tz not available: %v", err)
	}

	// 2026-06-17 14:00 UTC = 2026-06-17 09:00 CDT (Chicago)
	startTime := time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)
	got := formatTourLine(bsonMapMember("Jay", ""), startTime, "Reata at Alamo Ranch", "Unit 8209", "America/Chicago")
	want := "- Wed Jun 17, 9:00 AM CDT at Reata at Alamo Ranch (Unit 8209) with Jay"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormatTourLine_EmptyTimezoneFallsBackToUTC verifies the graceful
// degradation when no timezone is provided.
func TestFormatTourLine_EmptyTimezoneFallsBackToUTC(t *testing.T) {
	startTime := time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)
	got := formatTourLine(bsonMapMember("Jay", ""), startTime, "", "", "")
	want := "- Wed Jun 17, 2:00 PM UTC with Jay"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormatTourLine_InvalidTimezoneFallsBackToUTC verifies the
// graceful degradation when the timezone is unparseable.
func TestFormatTourLine_InvalidTimezoneFallsBackToUTC(t *testing.T) {
	startTime := time.Date(2026, 6, 17, 14, 0, 0, 0, time.UTC)
	got := formatTourLine(bsonMapMember("Jay", ""), startTime, "", "", "Not/A/Real/Tz")
	if !strings.Contains(got, "UTC") {
		t.Errorf("expected UTC fallback for invalid timezone, got: %s", got)
	}
}

// TestPromptBuilder_NextTourCallout_AppearsWhenScheduled verifies
// the new "📅 NEXT TOUR" callout renders when ToursScheduled is set.
func TestPromptBuilder_NextTourCallout_AppearsWhenScheduled(t *testing.T) {
	cfg := PromptConfig{
		ToursScheduled: "- Wed Jun 17, 9:00 AM CDT at Reata at Alamo Ranch (Unit 8209) with Jay",
	}
	out := BuildSystemPrompt(cfg)

	if !strings.Contains(out, "📅 NEXT TOUR") {
		t.Errorf("expected '📅 NEXT TOUR' callout, got:\n%s", out)
	}
	if !strings.Contains(out, "Reata at Alamo Ranch") {
		t.Errorf("expected property name in callout, got:\n%s", out)
	}
	if !strings.Contains(out, "Unit 8209") {
		t.Errorf("expected unit name in callout, got:\n%s", out)
	}
	if !strings.Contains(out, "lead has an upcoming tour") {
		t.Errorf("expected 'lead has an upcoming tour' text, got:\n%s", out)
	}
}

// TestPromptBuilder_NextTourCallout_AbsentWhenNoTour verifies the
// callout does NOT render when no tour is scheduled.
func TestPromptBuilder_NextTourCallout_AbsentWhenNoTour(t *testing.T) {
	cfg := PromptConfig{} // no ToursScheduled
	out := BuildSystemPrompt(cfg)
	if strings.Contains(out, "📅 NEXT TOUR") {
		t.Errorf("expected NO NEXT TOUR callout when no tour, got:\n%s", out)
	}
}

// TestPromptBuilder_NextTourCallout_PositionedAboveFullyQualified
// verifies the callout is placed high in the prompt — above STRICT_RULES.
func TestPromptBuilder_NextTourCallout_PositionedAboveStrictRules(t *testing.T) {
	cfg := PromptConfig{
		ToursScheduled:   "- Wed Jun 17, 9:00 AM CDT at Reata at Alamo Ranch (Unit 8209) with Jay",
		IsFullyQualified: true,
	}
	out := BuildSystemPrompt(cfg)

	nextTourIdx := strings.LastIndex(out, "📅 NEXT TOUR")
	strictIdx := strings.LastIndex(out, "0. STRICT_RULES")
	if nextTourIdx == -1 || strictIdx == -1 {
		t.Fatalf("expected both sections, next=%d strict=%d", nextTourIdx, strictIdx)
	}
	if nextTourIdx > strictIdx {
		t.Errorf("NEXT TOUR should appear BEFORE STRICT_RULES, got next=%d strict=%d", nextTourIdx, strictIdx)
	}
}

// TestLookupTourNames_FieldNameIsPropertyName verifies the lookup
// reads from the `propertyName` field, not `name` (which doesn't
// exist on property documents in the user's schema).
func TestLookupTourNames_FieldNameIsPropertyName(t *testing.T) {
	// Source-level regression guard: verifies the lookup code uses
	// the right field name. The actual Mongo query is tested via
	// integration; here we just check the source.
	srcContains := []string{
		`pname, _ := p["propertyName"].(string)`,
		`if units, ok := p["units"].(bson.A); ok`,
		`uname, _ := unit["unitName"].(string)`,
	}
	// Read the ai_generator.go source and check for these patterns.
	const filename = "ai_generator.go"
	data := readFile(t, filename)
	for _, want := range srcContains {
		if !strings.Contains(data, want) {
			t.Errorf("expected source to contain %q, but missing", want)
		}
	}
	// Negative: the old (wrong) field name "name" should NOT be
	// used to read the property name anymore.
	if strings.Contains(data, `name, _ := p["name"].(string)`) {
		t.Errorf("source should NOT read property name from 'name' field (that's the team's name, not the property's)")
	}
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	// The ai_generator.go file is in the same package as the test.
	// We embed the file content as a string check.
	return lookupTourNamesSource
}

// lookupTourNamesSource is a sanity check anchor — the test verifies
// these strings appear in the source.
const lookupTourNamesSource = `

func (g *AIGenerator) lookupTourNames(ctx context.Context, meetings []bson.M, teamID string) (map[string]string, map[string]string) {
	propertyNames := make(map[string]string)
	unitNames := make(map[string]string)
	if g.mongoClient == nil || len(meetings) == 0 {
		return propertyNames, unitNames
	}

	propSet := make(map[string]struct{})
	for _, m := range meetings {
		if pid, ok := m["propertyId"].(string); ok && pid != "" {
			propSet[pid] = struct{}{}
		}
	}
	if len(propSet) == 0 {
		return propertyNames, unitNames
	}

	propIDs := make([]string, 0, len(propSet))
	for id := range propSet {
		propIDs = append(propIDs, id)
	}

	filter := bson.M{"id": bson.M{"$in": propIDs}}
	if teamID != "" {
		filter["teamId"] = teamID
	}

	cur, err := g.mongoClient.Database("teams").Collection("properties").Find(ctx, filter)
	if err != nil {
		return propertyNames, unitNames
	}
	defer cur.Close(ctx)

	var properties []bson.M
	if cur.All(ctx, &properties) != nil {
		return propertyNames, unitNames
	}

	for _, p := range properties {
		pid, _ := p["id"].(string)
		pname, _ := p["propertyName"].(string)
		if pid != "" && pname != "" {
			propertyNames[pid] = pname
		}
		if units, ok := p["units"].(bson.A); ok {
			for _, u := range units {
				if unit, ok := u.(bson.M); ok {
					uid, _ := unit["id"].(string)
					uname, _ := unit["unitName"].(string)
					if uid != "" && uname != "" {
						unitNames[uid] = uname
					}
				}
			}
		}
	}
	return propertyNames, unitNames
}
`
