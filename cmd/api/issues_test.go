package main

import "testing"

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
