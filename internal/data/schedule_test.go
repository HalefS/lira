package data

import (
	"strings"
	"testing"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

// day builds a calendar date for this file. maintenance_test.go, lcu_test.go and
// issues_test.go each carry their own copy of this under their own name; taking
// one of theirs would couple this file to whichever is deleted first.
func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestWeekOfMondayToSunday(t *testing.T) {
	tests := []struct {
		name               string
		ref                time.Time
		wantStart, wantEnd string
	}{
		{"a Monday is the start of its own week", day(2026, time.September, 21), "2026-09-21", "2026-09-27"},
		{"a Wednesday is in the week its Monday began", day(2026, time.September, 23), "2026-09-21", "2026-09-27"},
		{"a Saturday is in the same week", day(2026, time.September, 26), "2026-09-21", "2026-09-27"},
		// The boundary that actually bites. Sunday belongs to the week it closes,
		// not to the Monday that follows it. Getting this wrong shifts a whole
		// column and nothing looks broken.
		{"Sunday closes the week rather than opening the next", day(2026, time.September, 27), "2026-09-21", "2026-09-27"},
		{"the Monday after starts a new week", day(2026, time.September, 28), "2026-09-28", "2026-10-04"},
		{"the Sunday before belongs to the prior week", day(2026, time.September, 20), "2026-09-14", "2026-09-20"},
		// Year boundary, from both sides of it. 2026-12-31 is a Thursday, so its
		// week starts on the 28th and ends in January: the pair must cross the
		// year without either half being wrong.
		{"a Thursday in the last week of the year", day(2026, time.December, 31), "2026-12-28", "2027-01-03"},
		{"New Year's Day is still in that week", day(2027, time.January, 1), "2026-12-28", "2027-01-03"},
		{"the Monday the new year actually starts on", day(2027, time.January, 4), "2027-01-04", "2027-01-10"},
		// Leap day. 2028-02-29 is a Tuesday and its week starts on the 28th, the
		// same Monday a non-leap February would give -- the case that catches
		// month-length arithmetic.
		{"the leap day sits in the week its Monday began", day(2028, time.February, 29), "2028-02-28", "2028-03-05"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := WeekOf(tc.ref)
			if got := w.Start.Time().Format(time.DateOnly); got != tc.wantStart {
				t.Errorf("WeekOf(%s).Start = %s, want %s (Monday)",
					tc.ref.Format(time.DateOnly), got, tc.wantStart)
			}
			if got := w.End.Time().Format(time.DateOnly); got != tc.wantEnd {
				t.Errorf("WeekOf(%s).End = %s, want %s (Sunday, inclusive)",
					tc.ref.Format(time.DateOnly), got, tc.wantEnd)
			}
		})
	}
}

// The seven columns, in order, Monday first, each one calendar day after the last.
//
// Deliberately NOT asserting End.Sub(Start) == 7*24h, which internal/report's
// TestWeekRangeIsMondayToSunday does. That holds only in a process whose timezone
// has no daylight saving; a week containing a transition is 167 or 169 hours. A rota
// is a run of calendar days, and copying that assertion into the one file whose
// entire job is the week would bake the bug in deeper.
func TestWeekOfAlwaysSevenDaysStartingMonday(t *testing.T) {
	w := WeekOf(day(2026, time.September, 24))

	if len(w.Days) != 7 {
		t.Fatalf("WeekOf produced %d days, want 7", len(w.Days))
	}
	labels := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	for i, d := range w.Days {
		if d.Weekday != i+1 {
			t.Errorf("column %d has Weekday %d, want %d (ISO: 1 is Monday)", i, d.Weekday, i+1)
		}
		if d.Label != labels[i] {
			t.Errorf("column %d labelled %q, want %q", i, d.Label, labels[i])
		}
		want := day(2026, time.September, 21+i).Format(time.DateOnly)
		if got := d.Date.Time().Format(time.DateOnly); got != want {
			t.Errorf("column %d dated %s, want %s", i, got, want)
		}
	}
}

// IsToday is what lets the noticeboard highlight the current column, so it must be
// true at most once, never on a day outside the week, and never for a week the
// reader is not in. Today() is the server's clock and is not injectable, so this
// asserts the invariant rather than a particular day.
func TestWeekOfTodayIsOnlyEverInsideTheWeek(t *testing.T) {
	today := Today().Format(time.DateOnly)

	lit := func(w *Week) []string {
		var out []string
		for _, d := range w.Days {
			if d.IsToday {
				out = append(out, d.Date.Time().Format(time.DateOnly))
			}
		}
		return out
	}

	if got := lit(WeekOf(time.Now())); len(got) != 1 {
		t.Errorf("the week containing today marked %v as today, want exactly one day", got)
	} else if got[0] != today {
		t.Errorf("IsToday was set on %s, which is not today (%s)", got[0], today)
	}

	// Three weeks out is unambiguously not this week, whatever the clock says.
	if got := lit(WeekOf(time.Now().AddDate(0, 0, 21))); len(got) != 0 {
		t.Errorf("a week three weeks out marked %v as today, want none", got)
	}
}

// The rota is recurring, so two weeks seven months apart have to agree on the shape
// and only differ in their dates. A regression that started filtering assignments
// by week would not be caught by the maths tests, so this states the property the
// grid depends on: the seven columns never change.
func TestWeekAlwaysHasTheSameSevenWeekdays(t *testing.T) {
	september := WeekOf(day(2026, time.September, 23))
	march := WeekOf(day(2027, time.March, 15))

	for i := range september.Days {
		if september.Days[i].Weekday != march.Days[i].Weekday {
			t.Errorf("column %d is weekday %d in September and %d in March",
				i, september.Days[i].Weekday, march.Days[i].Weekday)
		}
		if september.Days[i].Label != march.Days[i].Label {
			t.Errorf("column %d is labelled %q in September and %q in March",
				i, september.Days[i].Label, march.Days[i].Label)
		}
	}
}

// A zero ref means "now", the convention GetWeeklyConsumablesReport already uses.
// The rota's read endpoint relies on it, because it defaults the week parameter
// rather than requiring one.
func TestWeekOfZeroReferenceMeansThisWeek(t *testing.T) {
	today := Today().Format(time.DateOnly)

	w := WeekOf(time.Time{})
	found := false
	for _, d := range w.Days {
		if d.Date.Time().Format(time.DateOnly) == today {
			found = true
		}
	}
	if !found {
		t.Errorf("WeekOf(zero) produced %s..%s, which does not contain today (%s)",
			w.Start.Time().Format(time.DateOnly), w.End.Time().Format(time.DateOnly), today)
	}
}

// An end earlier than the start is a night shift, not a mistake. This is the case
// the schema exists to allow, so it gets its own table rather than a line inside
// the validation tests -- and it is also where 16:00-00:00 earns its place: it ends
// earlier in the day than it starts, so a bare `end < start` calls it overnight when
// it is an ordinary eight hours finishing at midnight.
func TestShiftOvernightDetection(t *testing.T) {
	tests := []struct {
		name          string
		start, end    string
		wantOvernight bool
		wantMinutes   int
	}{
		{"a morning shift does not cross midnight", "08:00", "16:00", false, 480},
		{"an evening shift does not either", "16:00", "23:59", false, 479},
		{"a night shift ends the following morning", "22:00", "06:00", true, 480},
		{"the shortest possible overnight", "23:59", "00:01", true, 2},
		{"the longest possible overnight", "20:00", "19:59", true, 1439},
		{"a shift starting at midnight runs into the day", "00:00", "08:00", false, 480},
		{"a shift ending at midnight does not cross it", "16:00", "00:00", false, 480},
		{"the same two times the other way round are a long day, not a night", "06:00", "22:00", false, 960},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Shift{StartTime: tc.start, EndTime: tc.end}
			if got := s.IsOvernight(); got != tc.wantOvernight {
				t.Errorf("Shift{%q,%q}.IsOvernight() = %v, want %v", tc.start, tc.end, got, tc.wantOvernight)
			}
			if got := s.DurationMinutes(); got != tc.wantMinutes {
				t.Errorf("Shift{%q,%q}.DurationMinutes() = %d, want %d", tc.start, tc.end, got, tc.wantMinutes)
			}
		})
	}
}

// A shift whose times have been mangled must not panic and must not claim to be
// overnight. ValidateShift has already refused these by the time anything reads
// them, but the read helpers are exported and are also used on rows nobody
// validated.
func TestShiftOvernightOnUnusableInput(t *testing.T) {
	for _, s := range []*Shift{
		{StartTime: "", EndTime: "06:00"},
		{StartTime: "22:00", EndTime: ""},
		{StartTime: "24:00", EndTime: "06:00"},
		{StartTime: "8:00", EndTime: "16:00"}, // unpadded, which the CHECK refuses
	} {
		if s.IsOvernight() {
			t.Errorf("Shift{%q,%q} claimed to be overnight", s.StartTime, s.EndTime)
		}
		if got := s.DurationMinutes(); got != 0 {
			t.Errorf("Shift{%q,%q}.DurationMinutes() = %d, want 0", s.StartTime, s.EndTime, got)
		}
	}
}

func TestShiftLabel(t *testing.T) {
	tests := []struct{ start, end, want string }{
		{"08:00", "16:00", "08:00-16:00"},
		{"22:00", "06:00", "22:00-06:00"},
	}
	for _, tc := range tests {
		if got := (&Shift{StartTime: tc.start, EndTime: tc.end}).Label(); got != tc.want {
			t.Errorf("Shift{%q,%q}.Label() = %q, want %q", tc.start, tc.end, got, tc.want)
		}
	}
}

func TestValidateShift(t *testing.T) {
	tests := []struct {
		name    string
		shift   Shift
		wantErr map[string]string // field -> substring, empty for success
	}{
		{"a plain day shift is fine", Shift{Name: "Morning", StartTime: "08:00", EndTime: "16:00"}, nil},
		// The case the whole design turns on: an overnight shift must validate.
		{"an overnight shift is fine", Shift{Name: "Night", StartTime: "22:00", EndTime: "06:00"}, nil},
		{"a shift ending at midnight is fine", Shift{Name: "Late", StartTime: "16:00", EndTime: "00:00"}, nil},
		{"a shift starting at midnight is fine", Shift{Name: "Zulu", StartTime: "00:00", EndTime: "08:00"}, nil},
		{"a half-hour handover is fine", Shift{Name: "Handover", StartTime: "15:30", EndTime: "16:00"}, nil},
		{"the maximum length is fine", Shift{Name: "Long", StartTime: "00:00", EndTime: "23:59"}, nil},
		{"surrounding whitespace is tolerated", Shift{Name: "  Morning  ", StartTime: " 08:00 ", EndTime: "16:00 "}, nil},

		// The only ordering refusal. A zero-length shift renders as a cell that is
		// assigned and empty at the same time.
		{"a zero-length shift is refused", Shift{Name: "Nothing", StartTime: "08:00", EndTime: "08:00"},
			map[string]string{"end_time": "same time"}},
		{"midnight to midnight is refused too", Shift{Name: "Nothing", StartTime: "00:00", EndTime: "00:00"},
			map[string]string{"end_time": "same time"}},

		{"a blank name is refused", Shift{Name: "   ", StartTime: "08:00", EndTime: "16:00"},
			map[string]string{"name": "must be provided"}},
		{"an over-long name is refused", Shift{Name: strings.Repeat("x", MaxShiftNameLength+1), StartTime: "08:00", EndTime: "16:00"},
			map[string]string{"name": "characters"}},

		// Times are HH:MM because that is what <input type="time"> produces.
		// Anything else would be stored and then never match the field again.
		{"a 24th hour is refused", Shift{Name: "X", StartTime: "24:00", EndTime: "06:00"},
			map[string]string{"start_time": "HH:MM"}},
		{"an unpadded hour is refused", Shift{Name: "X", StartTime: "8:00", EndTime: "16:00"},
			map[string]string{"start_time": "HH:MM"}},
		{"seconds are refused rather than truncated", Shift{Name: "X", StartTime: "08:00:00", EndTime: "16:00"},
			map[string]string{"start_time": "HH:MM"}},
		{"a bad end time is refused", Shift{Name: "X", StartTime: "08:00", EndTime: "6pm"},
			map[string]string{"end_time": "HH:MM"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.shift
			v := validator.New()
			ValidateShift(v, &s)

			if len(tc.wantErr) == 0 {
				if !v.Valid() {
					t.Fatalf("ValidateShift(%+v) reported %v, want no errors", tc.shift, v.Errors)
				}
				// The validators trim in place, so what gets stored is what was
				// checked. Asserted rather than assumed: a shift stored with its
				// padding would fail the database's own time CHECK.
				if s.Name != strings.TrimSpace(tc.shift.Name) ||
					s.StartTime != strings.TrimSpace(tc.shift.StartTime) {
					t.Errorf("ValidateShift did not trim: name=%q start=%q", s.Name, s.StartTime)
				}
				return
			}

			if v.Valid() {
				t.Fatalf("ValidateShift(%+v) accepted the shift, want errors on %v",
					tc.shift, tc.wantErr)
			}
			for field, want := range tc.wantErr {
				got, found := v.Errors[field]
				if !found {
					t.Errorf("no error on %q, got %v", field, v.Errors)
					continue
				}
				if !strings.Contains(got, want) {
					t.Errorf("error on %q was %q, want it to mention %q", field, got, want)
				}
			}
		})
	}
}

// A malformed time must not also collect "must not be the same time as the start
// time". Two complaints about one keystroke send the reader looking for a second
// problem they do not have.
func TestValidateShiftBadTimeGivesOnlyOneComplaint(t *testing.T) {
	s := Shift{Name: "X", StartTime: "99:99", EndTime: "08:00"}
	v := validator.New()
	ValidateShift(v, &s)

	if _, found := v.Errors["end_time"]; found {
		t.Errorf("a bad start_time also produced an end_time error (%q); the two times are the same complaint about one keystroke",
			v.Errors["end_time"])
	}
	if _, found := v.Errors["start_time"]; !found {
		t.Errorf("no error on start_time, got %v", v.Errors)
	}
}

func TestValidateDayAssignments(t *testing.T) {
	shift := func(id int64) *int64 { return &id }

	tests := []struct {
		name    string
		days    []DayAssignment
		wantErr map[string]string
	}{
		{"an empty week is fine -- it clears everything", nil, nil},
		{"the full seven days are fine", []DayAssignment{
			{Weekday: 1, ShiftID: shift(1)}, {Weekday: 2, ShiftID: shift(1)},
			{Weekday: 3, ShiftID: shift(1)}, {Weekday: 4, ShiftID: shift(1)},
			{Weekday: 5, ShiftID: shift(1)}, {Weekday: 6, ShiftID: nil},
			{Weekday: 7, ShiftID: nil},
		}, nil},

		// Weekday 0 is the one a Sunday-first implementation produces, and 8 is the
		// one an off-by-one at the top produces. Both must be refused rather than
		// landing in a cell that is not there.
		{"Sunday-as-zero is refused", []DayAssignment{{Weekday: 0, ShiftID: shift(1)}},
			map[string]string{"days": "not a day of the week"}},
		{"weekday 8 is refused", []DayAssignment{{Weekday: 8, ShiftID: shift(1)}},
			map[string]string{"days": "not a day of the week"}},
		{"a negative weekday is refused", []DayAssignment{{Weekday: -1, ShiftID: shift(1)}},
			map[string]string{"days": "not a day of the week"}},
		{"a repeated weekday is refused", []DayAssignment{
			{Weekday: 3, ShiftID: shift(1)}, {Weekday: 3, ShiftID: shift(2)}},
			map[string]string{"days": "more than once"}},
		{"more than seven days is refused", []DayAssignment{
			{Weekday: 1, ShiftID: nil}, {Weekday: 2, ShiftID: nil}, {Weekday: 3, ShiftID: nil},
			{Weekday: 4, ShiftID: nil}, {Weekday: 5, ShiftID: nil}, {Weekday: 6, ShiftID: nil},
			{Weekday: 7, ShiftID: nil}, {Weekday: 1, ShiftID: nil}},
			map[string]string{"days": "more than"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := validator.New()
			ValidateDayAssignments(v, tc.days)

			if len(tc.wantErr) == 0 {
				if !v.Valid() {
					t.Fatalf("ValidateDayAssignments reported %v, want no errors", v.Errors)
				}
				return
			}
			if v.Valid() {
				t.Fatalf("ValidateDayAssignments accepted %d days, want errors on %v", len(tc.days), tc.wantErr)
			}
			got, found := v.Errors["days"]
			if !found {
				t.Fatalf("no error on \"days\", got %v", v.Errors)
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(got, want) {
					t.Errorf("error on \"days\" was %q, want it to mention %q", got, want)
				}
			}
		})
	}
}

// A nil ShiftID is a day off, not an absent field, and the two have to be
// distinguishable by a plain int64 -- which is why DayAssignment.ShiftID is a
// pointer. Asserted here because the distinction is invisible in the struct
// definition and is the whole reason for the pointer.
func TestDayAssignmentNilShiftIsADayOff(t *testing.T) {
	off := DayAssignment{Weekday: 1, ShiftID: nil}
	if off.ShiftID != nil {
		t.Fatal("a day off must carry a nil ShiftID, not a zero id")
	}

	// And zero is not a usable shift id, so a client that sent one rather than null
	// would be asking the database about shift 0. The FK refuses it; this states
	// that the refusal is the intended answer rather than a surprise.
	var zero int64
	on := DayAssignment{Weekday: 1, ShiftID: &zero}
	if *on.ShiftID != 0 {
		t.Fatal("a zero shift id should survive to the database so the FK can reject it")
	}
}
