package generator

import (
	"testing"
	"time"

	"bamboo/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TestNormalizeQualificationDates_StringToTime verifies that an
// ISO string date is converted to a *time.Time.
func TestNormalizeQualificationDates_StringToTime(t *testing.T) {
	now := time.Now()
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{
				ID:     "q-1",
				Answer: "800-1000",
			},
		},
	}
	raw := bson.M{
		"qualificationQuestions": bson.A{
			bson.M{
				"id":          "q-1",
				"answer":      "800-1000",
				"answeredAt":  now.Format(time.RFC3339),
			},
		},
	}

	NormalizeQualificationDates(lead, raw)

	got := lead.QualificationQuestions[0].AnsweredAt
	if got == nil {
		t.Fatalf("expected AnsweredAt to be non-nil after normalizing string date")
	}
	// Compare with second precision since RFC3339 strings don't carry
	// sub-second precision (e.g., "2026-06-16T19:00:00Z" vs the full
	// time.Time with nanoseconds).
	if got.Unix() != now.Unix() {
		t.Errorf("expected AnsweredAt ~%v, got %v", now, *got)
	}
}

// TestNormalizeQualificationDates_BSONDate verifies that a proper
// BSON date is preserved (no-op).
func TestNormalizeQualificationDates_BSONDate(t *testing.T) {
	now := time.Now()
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q-1"},
		},
	}
	raw := bson.M{
		"qualificationQuestions": bson.A{
			bson.M{
				"id":         "q-1",
				"answeredAt": primitive.NewDateTimeFromTime(now),
			},
		},
	}

	NormalizeQualificationDates(lead, raw)

	got := lead.QualificationQuestions[0].AnsweredAt
	if got == nil {
		t.Fatalf("expected AnsweredAt to be non-nil after normalizing BSON date")
	}
	// Compare with second precision since BSON dates are ms precision.
	if got.Unix() != now.Unix() {
		t.Errorf("expected AnsweredAt ~%v, got %v", now, *got)
	}
}

// TestNormalizeQualificationDates_AskedAndAnswered verifies that
// BOTH askedAt and answeredAt get normalized.
func TestNormalizeQualificationDates_AskedAndAnswered(t *testing.T) {
	askedAt := time.Date(2026, 6, 16, 14, 51, 53, 0, time.UTC)
	answeredAt := time.Date(2026, 6, 16, 18, 59, 24, 0, time.UTC)
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q-1"},
		},
	}
	raw := bson.M{
		"qualificationQuestions": bson.A{
			bson.M{
				"id":         "q-1",
				"askedAt":    askedAt.Format(time.RFC3339),
				"answeredAt": answeredAt.Format(time.RFC3339),
			},
		},
	}

	NormalizeQualificationDates(lead, raw)

	got := lead.QualificationQuestions[0]
	if got.AskedAt == nil {
		t.Fatalf("expected AskedAt to be non-nil after normalization")
	}
	if !got.AskedAt.Equal(askedAt) {
		t.Errorf("expected AskedAt %v, got %v", askedAt, *got.AskedAt)
	}
	if got.AnsweredAt == nil {
		t.Fatalf("expected AnsweredAt to be non-nil after normalization")
	}
	if !got.AnsweredAt.Equal(answeredAt) {
		t.Errorf("expected AnsweredAt %v, got %v", answeredAt, *got.AnsweredAt)
	}
}

// TestNormalizeQualificationDates_MixedFormats verifies that
// mixed formats in the same document are all normalized.
func TestNormalizeQualificationDates_MixedFormats(t *testing.T) {
	ts := time.Date(2026, 6, 16, 19, 0, 0, 0, time.UTC)
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q-string-date"},
			{ID: "q-bson-date"},
			{ID: "q-no-date"},
		},
	}
	raw := bson.M{
		"qualificationQuestions": bson.A{
			bson.M{
				"id":         "q-string-date",
				"answeredAt": ts.Format(time.RFC3339), // ISO string
			},
			bson.M{
				"id":         "q-bson-date",
				"answeredAt": primitive.NewDateTimeFromTime(ts), // BSON date
			},
			bson.M{
				"id": "q-no-date", // no answeredAt
			},
		},
	}

	NormalizeQualificationDates(lead, raw)

	if lead.QualificationQuestions[0].AnsweredAt == nil {
		t.Errorf("string date should be normalized to *time.Time")
	}
	if lead.QualificationQuestions[1].AnsweredAt == nil {
		t.Errorf("BSON date should remain *time.Time")
	}
	if lead.QualificationQuestions[2].AnsweredAt != nil {
		t.Errorf("missing date should remain nil, got %v", *lead.QualificationQuestions[2].AnsweredAt)
	}
}

// TestNormalizeQualificationDates_NilInputs verifies safe behavior
// with nil inputs.
func TestNormalizeQualificationDates_NilInputs(t *testing.T) {
	// Should not panic.
	NormalizeQualificationDates(nil, nil)

	lead := &types.Lead{}
	NormalizeQualificationDates(lead, nil) // nil raw doc

	raw := bson.M{}
	NormalizeQualificationDates(lead, raw) // empty raw doc (no qualificationQuestions key)
}

// TestNormalizeQualificationDates_InvalidString verifies that an
// unparseable string date results in nil (not an error).
func TestNormalizeQualificationDates_InvalidString(t *testing.T) {
	lead := &types.Lead{
		QualificationQuestions: []types.QualificationQuestion{
			{ID: "q-bad-date"},
		},
	}
	raw := bson.M{
		"qualificationQuestions": bson.A{
			bson.M{
				"id":         "q-bad-date",
				"answeredAt": "not a valid date",
			},
		},
	}

	// Should not panic.
	NormalizeQualificationDates(lead, raw)

	if lead.QualificationQuestions[0].AnsweredAt != nil {
		t.Errorf("expected AnsweredAt to be nil for unparseable date, got %v",
			*lead.QualificationQuestions[0].AnsweredAt)
	}
}

// TestParseFlexibleDate_AllTypes verifies the type switch handles
// all the supported input types.
func TestParseFlexibleDate_AllTypes(t *testing.T) {
	ts := time.Date(2026, 6, 16, 19, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		in   interface{}
		want time.Time
	}{
		{"*time.Time", &ts, ts},
		{"time.Time", ts, ts},
		{"*primitive.DateTime", func() *primitive.DateTime { d := primitive.NewDateTimeFromTime(ts); return &d }(), ts},
		{"primitive.DateTime", primitive.NewDateTimeFromTime(ts), ts},
		{"ISO string", ts.Format(time.RFC3339), ts},
		{"*ISO string", func() *string { s := ts.Format(time.RFC3339); return &s }(), ts},
		{"empty string", "", time.Time{}},
		{"invalid string", "not a date", time.Time{}},
		{"nil *string", (*string)(nil), time.Time{}},
		{"nil *time.Time", (*time.Time)(nil), time.Time{}},
		{"nil *primitive.DateTime", (*primitive.DateTime)(nil), time.Time{}},
		{"nil", nil, time.Time{}},
		{"int (unsupported)", 12345, time.Time{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFlexibleDate(tt.in)
			if got == nil {
				if !tt.want.IsZero() {
					t.Errorf("expected %v, got nil", tt.want)
				}
				return
			}
			if tt.want.IsZero() {
				t.Errorf("expected nil, got %v", *got)
				return
			}
			// Compare with second precision.
			if got.Unix() != tt.want.Unix() {
				t.Errorf("expected %v, got %v", tt.want, *got)
			}
		})
	}
}
