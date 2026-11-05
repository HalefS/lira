package main

import (
	"testing"
	"time"
)

// A clock time has to be able to say three different things -- "leave it alone",
// "there is no time" and "here is the time" -- and the first two look identical
// if they are both collapsed to an empty value. Getting this wrong is what would
// leave a resolved issue's end time sitting on a row that has since been marked
// Pending.
func TestApplyClockTime(t *testing.T) {
	str := func(s string) *string { return &s }

	cases := []struct {
		name      string
		stored    *string
		submitted *string
		want      *string
	}{
		{
			name:      "a real time replaces the stored one",
			stored:    str("08:00"),
			submitted: str("14:30"),
			want:      str("14:30"),
		},
		{
			// Switching a resolved issue to Pending sends this.
			name:      "an empty string clears the stored time",
			stored:    str("14:30"),
			submitted: str(""),
			want:      nil,
		},
		{
			name:      "whitespace also clears it",
			stored:    str("14:30"),
			submitted: str("   "),
			want:      nil,
		},
		{
			name:      "an absent field leaves the stored time alone",
			stored:    str("14:30"),
			submitted: nil,
			want:      str("14:30"),
		},
		{
			name:      "an absent field on an already-empty time stays empty",
			stored:    nil,
			submitted: nil,
			want:      nil,
		},
		{
			name:      "a time can be set where there was none",
			stored:    nil,
			submitted: str("09:15"),
			want:      str("09:15"),
		},
		{
			name:      "clearing an already-empty time is harmless",
			stored:    nil,
			submitted: str(""),
			want:      nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stored := tc.stored
			applyClockTime(&stored, tc.submitted)

			switch {
			case stored == nil && tc.want == nil:
				// both empty, as wanted
			case stored == nil || tc.want == nil:
				t.Errorf("got %v, want %v", deref(stored), deref(tc.want))
			case *stored != *tc.want:
				t.Errorf("got %q, want %q", *stored, *tc.want)
			}
		})
	}
}

// On create there is nothing to leave alone, so blank simply means "no time" and
// the column is stored as NULL rather than an empty string.
func TestNormaliseClockTime(t *testing.T) {
	str := func(s string) *string { return &s }

	if got := normaliseClockTime(nil); got != nil {
		t.Errorf("nil -> %q, want nil", *got)
	}
	if got := normaliseClockTime(str("")); got != nil {
		t.Errorf("empty -> %q, want nil", *got)
	}
	if got := normaliseClockTime(str("  ")); got != nil {
		t.Errorf("whitespace -> %q, want nil", *got)
	}
	if got := normaliseClockTime(str("07:45")); got == nil || *got != "07:45" {
		t.Errorf("a real time was not preserved: %v", got)
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// An offline queue is only as good as the timestamp it carries. The window is
// deliberately generous in both directions: clocks drift, but a phone set to
// 2019 must not rewrite the report the issue appears in.
func TestClientCreatedAt(t *testing.T) {
	now := time.Date(2026, time.November, 5, 15, 0, 0, 0, time.UTC)
	within := now.Add(-2 * time.Hour).Format(time.RFC3339)

	got, valid, ignored := clientCreatedAt(within, now)
	if !valid || ignored {
		t.Fatalf("a recent timestamp should be accepted as-is, got valid=%v ignored=%v", valid, ignored)
	}
	if !got.Equal(now.Add(-2 * time.Hour)) {
		t.Errorf("got %v, want the parsed time", got)
	}

	if _, valid, _ := clientCreatedAt("not a timestamp", now); valid {
		t.Error("a non-RFC3339 value must be reported invalid")
	}

	if _, _, ignored := clientCreatedAt("2019-01-01T09:00:00Z", now); !ignored {
		t.Error("a timestamp from 2019 must be ignored, not stored")
	}
	if _, _, ignored := clientCreatedAt(now.Add(48*time.Hour).Format(time.RFC3339), now); !ignored {
		t.Error("a timestamp from next week must be ignored, not stored")
	}

	if _, valid, ignored := clientCreatedAt("   ", now); !valid || ignored {
		t.Error("blank is absent, not invalid and not ignored")
	}
}
