package helpers

import "testing"

func TestDetectHoursIntent_Positive(t *testing.T) {
	cases := []struct {
		name    string
		message string
	}{
		{"what are your hours", "What are your hours?"},
		{"when are you open", "when are you open"},
		{"what time do you open", "what time do you open"},
		{"office hours", "office hours"},
		{"business hours", "business hours"},
		{"leasing hours", "leasing hours"},
		{"are you open on saturday", "are you open on saturday"},
		{"what time can I come in", "what time can I come in"},
		{"hours of operation", "hours of operation"},
		{"what days are you open", "what days are you open"},
		{"are you open today", "are you open today"},
		{"are you open right now", "are you open right now"},
		{"when does the office close", "when does the office close"},
		{"leasing office hours question", "What are the leasing office hours?"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectHoursIntent(tc.message)
			if !got {
				t.Errorf("DetectHoursIntent(%q) = false, want true", tc.message)
			}
		})
	}
}

func TestDetectHoursIntent_Negative(t *testing.T) {
	cases := []struct {
		name    string
		message string
	}{
		{"schedule a tour", "I want to schedule a tour"},
		{"rent question", "How much is rent?"},
		{"bedroom question", "Do you have 2 bedroom apartments?"},
		{"application fee", "What's the application fee?"},
		{"pet question", "Can I bring my dog?"},
		{"parking question", "Is there parking?"},
		{"empty string", ""},
		{"hours in different context", "I need to move in 2 hours"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectHoursIntent(tc.message)
			if got {
				t.Errorf("DetectHoursIntent(%q) = true, want false", tc.message)
			}
		})
	}
}

func TestHoursDeflectionResponse_EnabledWithURL(t *testing.T) {
	url := "https://rentbamboo.com/schedule/abc123"
	got := HoursDeflectionResponse(true, url)

	if got == "" {
		t.Fatal("expected non-empty deflection response")
	}
	if !contains(got, url) {
		t.Errorf("expected response to contain the scheduling URL, got: %s", got)
	}
	if !contains(got, "hours") {
		t.Errorf("expected response to mention 'hours', got: %s", got)
	}
}

func TestHoursDeflectionResponse_DisabledOrNoURL(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		url     string
	}{
		{"disabled with url", false, "https://rentbamboo.com/schedule/abc123"},
		{"disabled no url", false, ""},
		{"enabled no url", true, ""},
		{"enabled whitespace url", true, "   "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HoursDeflectionResponse(tc.enabled, tc.url)
			if !contains(got, "team member will reach out") {
				t.Errorf("expected response to mention 'team member will reach out', got: %s", got)
			}
		})
	}
}

// contains is a tiny helper so we don't pull in strings just for tests.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
