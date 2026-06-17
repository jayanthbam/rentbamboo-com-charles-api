package helpers

import (
	"strings"
	"testing"
)

func TestReplaceLinkPlaceholders_SMS_BugReport(t *testing.T) {
	// Exact message from the bug report - should have [tour scheduling link] replaced.
	input := "Hi Tiara! Thanks for reaching out to Pillario! 🎉 We are located at Fishers, IN. I can help you schedule a tour, but first, do you have a current lease? If so, when does it end? Also, have you had any evictions in the past 2 years? Remember, all tours are open house style and last 15 minutes, so arrive on time! Please confirm your appointment to avoid cancellation. You can check availability through this link: [tour scheduling link]. Looking forward to your reply! -Pillario"
	scheduleURL := "https://rentbamboo.com/schedule/pillario-fishers"

	out := ReplaceLinkPlaceholders(input, scheduleURL, "", false)

	if strings.Contains(out, "[tour scheduling link]") {
		t.Errorf("placeholder was NOT replaced: %s", out)
	}
	if !strings.Contains(out, scheduleURL) {
		t.Errorf("expected URL %q in output, got: %s", scheduleURL, out)
	}
}

func TestReplaceLinkPlaceholders_SMSVariants(t *testing.T) {
	url := "https://example.com/sched/abc"
	cases := []string{
		"Schedule here: [SCHEDULE_LINK].",
		"Schedule here: [schedule link].",
		"Schedule here: [scheduling link].",
		"Schedule here: [tour link].",
		"Schedule here: [tour scheduling link].",
		"Schedule here: [schedule a tour link].",
		"Schedule here: [link to schedule a tour].",
		"Schedule here: [tour URL].",
		"Schedule here: [viewing link].",
	}
	for _, c := range cases {
		out := ReplaceLinkPlaceholders(c, url, "", false)
		if strings.Contains(out, "[") && strings.Contains(out, "]") {
			// Still contains a placeholder -> not replaced.
			t.Errorf("placeholder not replaced for input %q -> got %q", c, out)
		}
		if !strings.Contains(out, url) {
			t.Errorf("URL missing for input %q -> got %q", c, out)
		}
	}
}

func TestReplaceLinkPlaceholders_ApplicationVariants(t *testing.T) {
	url := "https://example.com/apply/abc"
	cases := []string{
		"Apply here: [APPLICATION_LINK].",
		"Apply here: [application link].",
		"Apply here: [application url].",
		"Apply here: [apply link].",
		"Apply here: [link to apply].",
	}
	for _, c := range cases {
		out := ReplaceLinkPlaceholders(c, "", url, false)
		if strings.Contains(out, "[") && strings.Contains(out, "]") {
			t.Errorf("placeholder not replaced for input %q -> got %q", c, out)
		}
		if !strings.Contains(out, url) {
			t.Errorf("URL missing for input %q -> got %q", c, out)
		}
	}
}

func TestReplaceLinkPlaceholders_HTMLMode(t *testing.T) {
	url := "https://example.com/sched/xyz"
	input := "<p>Schedule here: [tour scheduling link].</p>"
	out := ReplaceLinkPlaceholders(input, url, "", true)
	if !strings.Contains(out, `<a href="https://example.com/sched/xyz"`) {
		t.Errorf("expected <a> tag with href, got: %s", out)
	}
	if !strings.Contains(out, "target=\"_blank\"") {
		t.Errorf("expected target=_blank, got: %s", out)
	}
	if strings.Contains(out, "[tour scheduling link]") {
		t.Errorf("placeholder not replaced in HTML mode: %s", out)
	}
}

func TestReplaceLinkPlaceholders_NoURLStripsPlaceholder(t *testing.T) {
	input := "Schedule here: [tour scheduling link]. Thanks!"
	out := ReplaceLinkPlaceholders(input, "", "", false)
	if strings.Contains(out, "[tour scheduling link]") {
		t.Errorf("placeholder should be stripped when no URL: %s", out)
	}
	// Should tidy up the dangling ": ."
	if strings.Contains(out, ": .") {
		t.Errorf("expected tidied punctuation, got: %s", out)
	}
}

func TestReplaceLinkPlaceholders_LeavesNonLinkBracketsAlone(t *testing.T) {
	// Brackets that don't mention link/url/schedule/tour should be left alone.
	input := "Your property ID is [property123] and your balance is [unknown]."
	out := ReplaceLinkPlaceholders(input, "https://example.com", "https://apply.com", false)
	if !strings.Contains(out, "[property123]") || !strings.Contains(out, "[unknown]") {
		t.Errorf("non-link brackets should be preserved, got: %s", out)
	}
}

func TestReplaceLinkPlaceholders_EmptyInput(t *testing.T) {
	if got := ReplaceLinkPlaceholders("", "https://x", "", false); got != "" {
		t.Errorf("expected empty output, got: %q", got)
	}
}

func TestReplaceLinkPlaceholders_CaseInsensitive(t *testing.T) {
	url := "https://example.com/s/abc"
	cases := []string{
		"Here: [Tour Scheduling Link].",
		"Here: [TOUR LINK].",
		"Here: [Schedule_Link].",
	}
	for _, c := range cases {
		out := ReplaceLinkPlaceholders(c, url, "", false)
		if !strings.Contains(out, url) {
			t.Errorf("case-insensitive match failed for %q -> got %q", c, out)
		}
	}
}

// TestReplaceLinkPlaceholders_StripsStaleURLPlaceholder verifies that generic
// URL placeholders (which the AI might have copied from chat history where
// URLs were redacted) are stripped cleanly, NOT replaced with the current
// property's URL. The AI was quoting stale info — injecting a fresh URL would
// be misleading.
func TestReplaceLinkPlaceholders_StripsStaleURLPlaceholder(t *testing.T) {
	scheduleURL := "https://rentbamboo.com/schedule/current-property"
	cases := []struct {
		name  string
		input string
	}{
		{"bare [URL]", "Lock it in: [URL] See you then!"},
		{"[stale-link]", "Lock it in: [stale-link] See you then!"},
		{"[STALE-LINK] case-insensitive", "Lock it in: [STALE-LINK] See you then!"},
		{"[stale_url] with underscore", "Lock it in: [stale_url] See you then!"},
		{"[old-link] variant", "Lock it in: [old-link] See you then!"},
		{"[old_url] variant", "Lock it in: [old_url] See you then!"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := ReplaceLinkPlaceholders(c.input, scheduleURL, "", false)
			if strings.Contains(out, "[URL]") || strings.Contains(out, "[stale-link]") ||
				strings.Contains(out, "[stale_url]") || strings.Contains(out, "[old-link]") ||
				strings.Contains(out, "[old_url]") {
				t.Errorf("stale placeholder not stripped: %s", out)
			}
			if strings.Contains(out, scheduleURL) {
				t.Errorf("current URL should NOT be injected when stripping stale placeholder, got: %s", out)
			}
		})
	}
}

// TestReplaceLinkPlaceholders_StalePlaceholderTidiesPunctuation verifies
// that stripping a stale placeholder cleans up the surrounding whitespace
// and dangling punctuation so the result remains grammatical.
func TestReplaceLinkPlaceholders_StalePlaceholderTidiesPunctuation(t *testing.T) {
	input := "Lock it in: [URL] See you then!"
	out := ReplaceLinkPlaceholders(input, "https://example.com/sched", "", false)
	// Should not have ": ." (the colon from "Lock it in:" with stripped URL
	// and no period before "See").
	if strings.Contains(out, ": .") {
		t.Errorf("expected tidied punctuation, got: %s", out)
	}
	// Should not have double spaces from the stripped placeholder.
	if strings.Contains(out, "  ") {
		t.Errorf("expected no double spaces, got: %s", out)
	}
	// Should still have "See you then!" intact.
	if !strings.Contains(out, "See you then!") {
		t.Errorf("expected trailing text preserved, got: %s", out)
	}
}

// TestReplaceLinkPlaceholders_CanonicalStillReplacesAfterStaleStrip verifies
// that canonical placeholders like [SCHEDULE_LINK] are still replaced with
// the current URL even when the input also contains stale placeholders
// elsewhere in the text.
func TestReplaceLinkPlaceholders_CanonicalStillReplacesAfterStaleStrip(t *testing.T) {
	scheduleURL := "https://rentbamboo.com/schedule/abc"
	input := "The old link was [URL] but the new one is [SCHEDULE_LINK]."
	out := ReplaceLinkPlaceholders(input, scheduleURL, "", false)
	if strings.Contains(out, "[URL]") {
		t.Errorf("stale placeholder should be stripped: %s", out)
	}
	if strings.Contains(out, "[SCHEDULE_LINK]") {
		t.Errorf("canonical placeholder should be replaced: %s", out)
	}
	if !strings.Contains(out, scheduleURL) {
		t.Errorf("expected current URL, got: %s", out)
	}
}
