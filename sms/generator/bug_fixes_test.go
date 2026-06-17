package generator

import (
	"testing"
)

// TestNormalizePhone_StripsUSCountryCode covers the standard US phone
// normalization (strips +1 or leading 1) used by tour/lead lookups.
func TestNormalizePhone_StripsUSCountryCode(t *testing.T) {
	cases := map[string]string{
		"+12102743516": "2102743516",
		"12102743516":  "2102743516",
		"2102743516":   "2102743516",
		"  +12102743516  ": "2102743516",
		"":             "",
		"12345":        "12345", // too short to strip
	}
	for in, want := range cases {
		if got := normalizePhone(in); got != want {
			t.Errorf("normalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBuildPhoneVariants_AllFormatsIncluded verifies the phone variant
// generator produces all common US phone formats (10-digit, 1+10-digit,
// +1+10-digit) for a 10-digit normalized phone. This is what the
// fetchLeadTours OR-clause relies on to find meetings stored with
// inconsistent phone formats.
func TestBuildPhoneVariants_AllFormatsIncluded(t *testing.T) {
	// Simulate the variant-building logic from fetchLeadTours.
	buildVariants := func(leadPhone string) []string {
		normalized := normalizePhone(leadPhone)
		variants := []string{normalized}
		if normalized != "" && normalized != leadPhone {
			variants = append(variants, leadPhone)
		}
		if normalized != "" && len(normalized) == 10 {
			variants = append(variants, "1"+normalized, "+1"+normalized)
		}
		return variants
	}

	cases := []struct {
		input    string
		mustHave []string
	}{
		{
			input:    "+12102743516",
			mustHave: []string{"2102743516", "12102743516", "+12102743516"},
		},
		{
			input:    "2102743516",
			mustHave: []string{"2102743516", "12102743516", "+12102743516"},
		},
		{
			input:    "12102743516",
			mustHave: []string{"2102743516", "12102743516", "+12102743516"},
		},
		{
			input:    "12345",
			mustHave: []string{"12345"},
		},
		{
			input:    "",
			mustHave: nil,
		},
	}
	for _, c := range cases {
		got := buildVariants(c.input)
		for _, want := range c.mustHave {
			if !contains(got, want) {
				t.Errorf("buildVariants(%q) = %v, missing %q", c.input, got, want)
			}
		}
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
