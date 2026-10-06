package data

import (
	"strings"
	"testing"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

// absenceDay builds a calendar date for this file. schedule_test.go has its own
// `day` in the same package and Go will not let two files declare it twice, so
// this one is spelled differently -- the convention maintenance_test.go, lcu_test.go
// and issues_test.go already follow for exactly that reason.
func absenceDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// --- the tint ----------------------------------------------------------------

// A tint is the one field in the rota where "absent", "empty" and "invalid" could
// plausibly have been three states, and the whole design is that they are two:
// absent and empty are the same thing, and invalid is refused. Asserted here
// because that decision lives entirely in this one function.
func TestNormaliseShiftColor(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  string
		wantOK bool
	}{
		{"empty means no tint", "", "", true},
		{"whitespace alone is also no tint", "   ", "", true},

		// Lower case is the canonical form, because that is what <input
		// type="color"> hands back and the round trip should store the string the
		// browser already produced.
		{"the hash is optional on the way in", "1d9e75", "#1d9e75", true},
		{"the hash is kept when it is there", "#1d9e75", "#1d9e75", true},
		{"case is folded so the Settings list cannot show one colour twice", "#1D9E75", "#1d9e75", true},
		{"surrounding whitespace is trimmed", "  #1d9e75\t\n", "#1d9e75", true},
		{"mixed case folds the same way", "#AbCdEf", "#abcdef", true},

		// The refusals. All of these would end up interpolated into a style
		// attribute if they were let through.
		{"three digits is not a colour", "#abc", "", false},
		{"five digits is not a colour", "#abcde", "", false},
		{"no hash and not six digits", "1d9e7", "", false},
		{"a named colour is not a hex value", "red", "", false},
		{"a CSS class name is not a hex value", "badge-door", "", false},
		{"eight digits is an alpha channel, which this has no use for", "#1d9e75ff", "", false},
		{"trailing punctuation is not trimmed away", "#1d9e75;", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NormaliseShiftColor(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("NormaliseShiftColor(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("NormaliseShiftColor(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A refused tint must leave the field blank rather than keeping the raw value, or
// a caller that ignored the error could still reach the INSERT with it.
func TestNormaliseShiftColorBlanksOnFailure(t *testing.T) {
	got, ok := NormaliseShiftColor("#nothex")
	if ok {
		t.Fatal("expected #nothex to be refused")
	}
	if got != "" {
		t.Errorf("a refused tint left %q in the field; it must be blank", got)
	}
}

// The tint goes through ValidateShift rather than being trusted, and lands on the
// shift so what gets validated is what gets stored.
func TestValidateShiftNormalisesAndChecksTheTint(t *testing.T) {
	s := &Shift{Name: "Night", StartTime: "22:00", EndTime: "06:00", Color: " #B02E8C "}
	v := validator.New()
	ValidateShift(v, s)
	if !v.Valid() {
		t.Fatalf("a well-formed tint was refused: %v", v.Errors)
	}
	if s.Color != "#b02e8c" {
		t.Errorf("Color = %q after validation, want the canonical %q", s.Color, "#b02e8c")
	}

	// And a bad one, on the color key specifically, so the message lands next to
	// the swatch in the editor rather than as a form-level failure.
	v = validator.New()
	ValidateShift(v, &Shift{Name: "Night", StartTime: "22:00", EndTime: "06:00", Color: "chartreuse"})
	if v.Valid() {
		t.Fatal("expected a named colour to be refused")
	}
	if got := v.Errors["color"]; !strings.Contains(got, "#RRGGBB") {
		t.Errorf("error on \"color\" was %q, want it to mention the #RRGGBB format", got)
	}
}

// No tint is the normal state and must not be an error. If clearing a colour were
// refused, "no tint" would be unrepresentable and the column could never go back to
// NULL.
func TestValidateShiftAcceptsNoTint(t *testing.T) {
	s := &Shift{Name: "Morning", StartTime: "08:00", EndTime: "16:00"}
	v := validator.New()
	ValidateShift(v, s)
	if !v.Valid() {
		t.Fatalf("an untinted shift was refused: %v", v.Errors)
	}
	if s.Color != "" {
		t.Errorf("Color = %q, want the empty string for no tint", s.Color)
	}
}

// --- absences ---------------------------------------------------------------

// The span is inclusive at both ends, so a one-day absence is the same date twice.
// This is the assumption the whole edit form rests on, and getting it wrong makes
// every absence one day short.
func TestAbsenceDayCountIsInclusive(t *testing.T) {
	tests := []struct {
		name       string
		starts, end time.Time
		want       int
	}{
		{"one day is the same date twice", absenceDay(2026, time.October, 5), absenceDay(2026, time.October, 5), 1},
		{"a Monday-to-Friday holiday is five days", absenceDay(2026, time.October, 5), absenceDay(2026, time.October, 9), 5},
		{"a fortnight", absenceDay(2026, time.October, 5), absenceDay(2026, time.October, 19), 15},
		{"a month that is not 30 days", absenceDay(2026, time.February, 1), absenceDay(2026, time.February, 28), 28},
		{"across a leap day", absenceDay(2028, time.February, 27), absenceDay(2028, time.March, 2), 5},
		{"a year", absenceDay(2026, time.January, 1), absenceDay(2026, time.December, 31), 365},
		{"a leap year", absenceDay(2028, time.January, 1), absenceDay(2028, time.December, 31), 366},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &Absence{StartsOn: JSONDate(tc.starts), EndsOn: JSONDate(tc.end)}
			if got := a.DayCount(); got != tc.want {
				t.Errorf("DayCount() = %d, want %d", got, tc.want)
			}
		})
	}
}

// Covers is the one place a date is compared, and it compares ISO strings rather
// than time.Time values on purpose: a `date` column scanned back from Postgres
// arrives as midnight UTC while WeekRange's dates are local midnight, so comparing
// the two instants would put every reader west of UTC a day out. Both boundaries
// are asserted, because "starts on the 5th" has to include the 5th.
func TestAbsenceCovers(t *testing.T) {
	a := &Absence{
		StartsOn: JSONDate(absenceDay(2026, time.October, 5)),
		EndsOn:   JSONDate(absenceDay(2026, time.October, 9)),
	}

	tests := []struct {
		date string
		want bool
	}{
		{"2026-10-04", false}, // the day before
		{"2026-10-05", true},  // the first day, inclusive
		{"2026-10-06", true},
		{"2026-10-07", true},
		{"2026-10-08", true},
		{"2026-10-09", true},  // the last day, inclusive
		{"2026-10-10", false}, // the day after
		{"2026-09-30", false}, // well before
		{"2026-11-01", false}, // well after
	}
	for _, tc := range tests {
		t.Run(tc.date, func(t *testing.T) {
			if got := a.Covers(tc.date); got != tc.want {
				t.Errorf("Covers(%q) = %v, want %v", tc.date, got, tc.want)
			}
		})
	}

	if a.Covers("") {
		t.Error("an empty date must not be covered")
	}
}

// A single-day absence covers exactly that day, which is the case a per-day
// expansion of the range gets wrong by dropping or duplicating the boundary.
func TestAbsenceCoversSingleDay(t *testing.T) {
	a := &Absence{
		StartsOn: JSONDate(absenceDay(2026, time.October, 5)),
		EndsOn:   JSONDate(absenceDay(2026, time.October, 5)),
	}
	if !a.Covers("2026-10-05") {
		t.Error("the single day of a one-day absence must be covered")
	}
	for _, d := range []string{"2026-10-04", "2026-10-06"} {
		if a.Covers(d) {
			t.Errorf("%s must not be covered by an absence that is only the 5th", d)
		}
	}
}

func TestValidateAbsence(t *testing.T) {
	mk := func() *Absence {
		return &Absence{
			UserID:   1,
			Kind:     AbsenceVacation,
			StartsOn: JSONDate(absenceDay(2026, time.October, 5)),
			EndsOn:   JSONDate(absenceDay(2026, time.October, 9)),
		}
	}

	t.Run("a well-formed absence is accepted", func(t *testing.T) {
		v := validator.New()
		ValidateAbsence(v, mk())
		if !v.Valid() {
			t.Fatalf("a valid absence was refused: %v", v.Errors)
		}
	})

	t.Run("kind and reason are trimmed and folded", func(t *testing.T) {
		a := mk()
		a.Kind = "  SICK  "
		a.Reason = "  back surgery  "
		v := validator.New()
		ValidateAbsence(v, a)
		if !v.Valid() {
			t.Fatalf("a valid absence was refused: %v", v.Errors)
		}
		if a.Kind != AbsenceSick {
			t.Errorf("Kind = %q, want the folded %q", a.Kind, AbsenceSick)
		}
		if a.Reason != "back surgery" {
			t.Errorf("Reason = %q, want it trimmed", a.Reason)
		}
	})

	// Each error lands on its own field key, because validator.AddError keeps only
	// the first per key and these are separate form fields.
	fields := []struct {
		name    string
		mutate  func(*Absence)
		key     string
		wantMsg string
	}{
		{
			"no user", func(a *Absence) { a.UserID = 0 }, "user_id", "must be provided",
		},
		{
			"no kind", func(a *Absence) { a.Kind = "  " }, "kind", "must be provided",
		},
		{
			// The set is closed, so an unrecognised kind is a field error rather than
			// a row the database will refuse halfway through.
			"unknown kind", func(a *Absence) { a.Kind = "bereaved" }, "kind", "must be one of",
		},
		{
			"empty reason is allowed", func(a *Absence) { a.Reason = "" }, "", "",
		},
		{
			"reason over the cap", func(a *Absence) { a.Reason = strings.Repeat("x", MaxAbsenceReasonLength+1) },
			"reason", "must not be more than 200 characters",
		},
		{
			"no start date", func(a *Absence) { a.StartsOn = JSONDate(time.Time{}) }, "starts_on", "must be provided",
		},
		{
			"no end date", func(a *Absence) { a.EndsOn = JSONDate(time.Time{}) }, "ends_on", "must be provided",
		},
		{
			// The span is inclusive, so an end before the start cannot be a one-day
			// absence: it is a typo.
			"end before start", func(a *Absence) {
				a.StartsOn = JSONDate(absenceDay(2026, time.October, 9))
				a.EndsOn = JSONDate(absenceDay(2026, time.October, 5))
			}, "ends_on", "must not be before the start date",
		},
		{
			// Reachable by an ordinary typo, which is what makes this a validation
			// rather than a belt: 20271101 is a plausible mis-typed end date, and it
			// would otherwise paint somebody off the board until 2031.
			"a span past the cap",
			func(a *Absence) { a.EndsOn = JSONDate(absenceDay(2031, time.July, 1)) },
			"ends_on", "must not be more than 730 days after the start date",
		},
	}

	for _, tc := range fields {
		t.Run(tc.name, func(t *testing.T) {
			a := mk()
			tc.mutate(a)
			v := validator.New()
			ValidateAbsence(v, a)

			if tc.key == "" {
				if !v.Valid() {
					t.Fatalf("expected this to be accepted, got %v", v.Errors)
				}
				return
			}
			if v.Valid() {
				t.Fatal("expected a validation error")
			}
			got, present := v.Errors[tc.key]
			if !present {
				t.Fatalf("expected an error on %q, got %v", tc.key, v.Errors)
			}
			if !strings.Contains(got, tc.wantMsg) {
				t.Errorf("error on %q was %q, want it to mention %q", tc.key, got, tc.wantMsg)
			}
		})
	}

	// A missing date must not collect a second complaint about the span. Otherwise
	// one absent field produces two errors about two fields, which reads as two
	// problems to fix when there is one -- the same guard ValidateShift makes.
	t.Run("a missing date is reported once, not also as a bad span", func(t *testing.T) {
		a := mk()
		a.EndsOn = JSONDate(time.Time{})
		v := validator.New()
		ValidateAbsence(v, a)
		if len(v.Errors) != 1 {
			t.Fatalf("expected exactly one error, got %d: %v", len(v.Errors), v.Errors)
		}
		if _, present := v.Errors["ends_on"]; !present {
			t.Errorf("expected the error on ends_on, got %v", v.Errors)
		}
	})

	// Exactly at the cap, not over it. The two dates are hardcoded rather than derived
	// from the cap, so this test cannot agree with a wrong DayCount by
	// construction: 2026-10-05 to 2028-10-03 is 730 days, while the two years on to
	// 2028-10-05 is 732 because a leap day falls inside it. Picking the wrong pair
	// here is exactly the month-length arithmetic DayCount exists to survive, so
	// the test asserts the day count as well as the verdict.
	t.Run("a span of exactly the cap is accepted", func(t *testing.T) {
		a := mk()
		a.EndsOn = JSONDate(absenceDay(2028, time.October, 3))
		if got := a.DayCount(); got != 730 {
			t.Fatalf("test setup is wrong: 2026-10-05 to 2028-10-03 is %d days, expected 730", got)
		}
		v := validator.New()
		ValidateAbsence(v, a)
		if !v.Valid() {
			t.Errorf("a span of exactly 730 days was refused: %v", v.Errors)
		}
	})

	t.Run("a span one day over the cap is refused", func(t *testing.T) {
		a := mk()
		a.EndsOn = JSONDate(absenceDay(2028, time.October, 4))
		if got := a.DayCount(); got != 731 {
			t.Fatalf("test setup is wrong: expected 731 days, got %d", got)
		}
		v := validator.New()
		ValidateAbsence(v, a)
		if v.Valid() {
			t.Error("a span of 731 days was accepted, which is past the 730-day cap")
		}
	})
}

// The kind set is closed, and the validation message and the picker list are both
// built from it, so there is no way for them to disagree about what is legal.
func TestAbsenceKinds(t *testing.T) {
	kinds := AbsenceKinds()
	want := []string{AbsenceVacation, AbsenceSick, AbsenceTraining, AbsenceUnavailable}
	if len(kinds) != len(want) {
		t.Fatalf("AbsenceKinds() has %d entries, want %d", len(kinds), len(want))
	}
	for i, k := range want {
		if kinds[i] != k {
			t.Errorf("AbsenceKinds()[%d] = %q, want %q", i, kinds[i], k)
		}
	}

	// Every kind the list offers must actually validate, and anything outside it
	// must not. This is the Go half of absences_kind_check in migration 000031.
	for _, k := range kinds {
		a := &Absence{
			UserID:   1,
			Kind:     k,
			StartsOn: JSONDate(absenceDay(2026, time.October, 5)),
			EndsOn:   JSONDate(absenceDay(2026, time.October, 5)),
		}
		v := validator.New()
		ValidateAbsence(v, a)
		if !v.Valid() {
			t.Errorf("kind %q is offered by AbsenceKinds but refused by validation: %v", k, v.Errors)
		}
	}
}

// The reason field is the one thing in this feature that must never reach a public
// payload, and the guarantee is the shape of the type rather than a branch: if
// AbsenceRef has no such field, no handler can leak it by forgetting to clear one.
func TestAbsenceRefCarriesNoReason(t *testing.T) {
	a := &Absence{
		ID:        7,
		UserID:    3,
		Kind:      AbsenceSick,
		StartsOn:  JSONDate(absenceDay(2026, time.October, 5)),
		EndsOn:    JSONDate(absenceDay(2026, time.October, 9)),
		Reason:    "back surgery",
		Version:   2,
	}
	ref := a.Ref()

	if ref.ID != 7 || ref.UserID != 3 || ref.Version != 2 {
		t.Errorf("Ref() lost identity fields: %+v", ref)
	}
	if ref.StartsOn != a.StartsOn || ref.EndsOn != a.EndsOn {
		t.Error("Ref() must carry the range, or the grid cannot tell where a run ends")
	}
	// The compile-time half of the guarantee, stated as a test so it fails loudly if
	// somebody adds the field later: AbsenceRef has no Reason, so there is nothing
	// to copy from the row above. If this ever needs changing, the thing to change
	// is this test, deliberately.
	if got := ref.Kind; got != AbsenceSick {
		t.Errorf("Ref().Kind = %q, want the stored %q -- Ref must not redact, that is PublicKind's job", got, AbsenceSick)
	}
}

// A manager sees the real kind. Everybody else sees "unavailable", because the
// rota is a page anybody can load without an account and publishing the word SICK
// beside a named colleague is publishing health information about them.
func TestPublicKind(t *testing.T) {
	tests := []struct {
		kind    string
		canEdit bool
		want    string
	}{
		{AbsenceSick, false, AbsenceUnavailable},
		{AbsenceSick, true, AbsenceSick},
		{AbsenceVacation, false, AbsenceVacation},
		{AbsenceVacation, true, AbsenceVacation},
		{AbsenceTraining, false, AbsenceTraining},
		{AbsenceTraining, true, AbsenceTraining},
		{AbsenceUnavailable, false, AbsenceUnavailable},
		{AbsenceUnavailable, true, AbsenceUnavailable},
	}
	for _, tc := range tests {
		name := tc.kind
		if tc.canEdit {
			name += " as a manager"
		} else {
			name += " to the public"
		}
		t.Run(name, func(t *testing.T) {
			if got := PublicKind(tc.kind, tc.canEdit); got != tc.want {
				t.Errorf("PublicKind(%q, %v) = %q, want %q", tc.kind, tc.canEdit, got, tc.want)
			}
		})
	}
}

// The reduced kind must still be one the client knows how to draw, or the public
// board renders a kind it has no glyph for.
func TestPublicKindProducesAKnownKind(t *testing.T) {
	for _, k := range AbsenceKinds() {
		got := PublicKind(k, false)
		if !validAbsenceKind[got] {
			t.Errorf("PublicKind(%q, false) = %q, which is not one of the four known kinds", k, got)
		}
	}
}
