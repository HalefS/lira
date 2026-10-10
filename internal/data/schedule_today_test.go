package data

import (
	"testing"
	"time"
)

// The rota is RECURRING -- shift_assignments is keyed on (user_id, weekday) with no
// date at all -- which is what makes "who is on right now" a two-cell question rather
// than a one-cell one. These tests are the only place that is exercised: the fixture
// database has weekday-1-to-5 assignments and the clock moves too slowly to reach a
// Tuesday-at-01:00 moment on demand.
//
// Live row 1 of shift_assignments is the exact shape: user 1 on Night (22:00-06:00) for
// weekday 1, and Morning for weekdays 2-5.

func at(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.Local)
}

// nightThenMorning is a member on a 22:00-06:00 shift for Monday and an 08:00-16:00
// shift for the days either side, which is the live shape.
func nightThenMorning() *ScheduleMember {
	m := &ScheduleMember{UserID: 1, Name: "Halef Spencer"}
	m.Days[0] = &Shift{Name: "Night", StartTime: "22:00", EndTime: "06:00"}   // Monday
	m.Days[1] = &Shift{Name: "Morning", StartTime: "08:00", EndTime: "16:00"} // Tuesday
	return m
}

// THE REGRESSION THIS EXISTS FOR.
//
// At 01:00 on Tuesday this member is three hours into Monday's overnight. Reading only
// Tuesday's cell says "Morning, starts at 08:00" -- off shift, and about to start a
// shift they have been working for three hours.
func TestStatusAtReadsYesterdaysCellForAnOvernight(t *testing.T) {
	// 2026-10-06 is a Tuesday; the 5th is Monday.
	tue := at(2026, time.October, 6, 1, 0)
	if got := nightThenMorning().StatusAt(tue, false); got != TodayWorkingNow {
		t.Errorf("at Tue 01:00, status = %q, want working_now -- the member is three "+
			"hours into MONDAY's 22:00-06:00 shift and today's cell is the wrong one", got)
	}
}

// Even with NO cell today at all the answer is still working_now, which is the case a
// single-cell implementation cannot distinguish from "not rostered".
func TestStatusAtOvernightSurvivesAnEmptyTodayCell(t *testing.T) {
	m := &ScheduleMember{UserID: 1, Name: "Halef Spencer"}
	m.Days[0] = &Shift{Name: "Night", StartTime: "22:00", EndTime: "06:00"} // Monday only
	if got := m.StatusAt(at(2026, time.October, 6, 1, 0), false); got != TodayWorkingNow {
		t.Errorf("status = %q, want working_now; today's cell is nil and yesterday's "+
			"overnight still covers this instant", got)
	}
}

// THE WRAP. Yesterday's index for Monday is 6 -- Sunday -- so a Sunday night shift
// covers Monday at 00:30, and it is the only reason it does.
//
// The first version of this test asserted the OPPOSITE ("Monday 00:30 must not pick up
// Sunday's shift"), which would have been a test demanding the bug. The wrap is the
// feature: without it a Sunday 22:00-06:00 member vanishes for six hours every Monday.
func TestStatusAtWrapsToSundaysOvernightOnAMondayMorning(t *testing.T) {
	m := &ScheduleMember{UserID: 1, Name: "Sunday night"}
	m.Days[6] = &Shift{Name: "Sunday night", StartTime: "22:00", EndTime: "06:00"} // Sunday

	if got := m.StatusAt(at(2026, time.October, 5, 0, 30), false); got != TodayWorkingNow {
		t.Errorf("Monday 00:30 status = %q, want working_now; Sunday's 22:00-06:00 shift "+
			"reaches into Monday and the weekday index must wrap to reach it", got)
	}
	// And the same member at 07:00 Monday has knocked off, so the wrap does not leave
	// them on forever.
	if got := m.StatusAt(at(2026, time.October, 5, 7, 0), false); got != TodayNotRostered {
		t.Errorf("Monday 07:00 status = %q, want not_rostered; the overnight ended at 06:00", got)
	}

	// A member whose own cell has not started yet must not be rescued by the wrap.
	early := &ScheduleMember{UserID: 2, Name: "Monday morning"}
	early.Days[0] = &Shift{Name: "Monday morning", StartTime: "08:00", EndTime: "16:00"}
	if got := early.StatusAt(at(2026, time.October, 5, 0, 30), false); got != TodayComingSoon {
		t.Errorf("Monday 00:30 status = %q, want coming_soon; the shift starts at 08:00 "+
			"and nothing has carried over", got)
	}
}

func TestStatusAtOrdinaryDay(t *testing.T) {
	m := nightThenMorning()
	for _, tc := range []struct {
		name string
		now  time.Time
		want TodayStatus
	}{
		{"before the shift", at(2026, time.October, 6, 7, 0), TodayComingSoon},
		{"at the start minute", at(2026, time.October, 6, 8, 0), TodayWorkingNow},
		{"mid shift", at(2026, time.October, 6, 12, 0), TodayWorkingNow},
		{"last minute of shift", at(2026, time.October, 6, 15, 59), TodayWorkingNow},
		{"at the end minute", at(2026, time.October, 6, 16, 0), TodayFinished},
		{"after it", at(2026, time.October, 6, 17, 0), TodayFinished},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.StatusAt(tc.now, false); got != tc.want {
				t.Errorf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

// An absence is the dated EXCEPTION to the recurring pattern, so it beats the cell.
// Showing somebody on confirmed sick leave as "working now" would be a factual error
// about a colleague.
func TestStatusAtAbsenceBeatsEverything(t *testing.T) {
	m := nightThenMorning()
	if got := m.StatusAt(at(2026, time.October, 6, 12, 0), true); got != TodayAway {
		t.Errorf("status = %q, want away", got)
	}
}

// THE CASE IsOvernight CANNOT ANSWER, which is why Window exists and IsOvernight is not
// used for this. 16:00-00:00 is NOT overnight by that definition -- its end is midnight,
// so `end > 0` is false -- yet its end instant really is the following midnight. The two
// disagreeing is the whole trap.
func TestWindowAndIsOvernightDisagreeForTheMidnightShift(t *testing.T) {
	day := at(2026, time.October, 10, 0, 0)
	s := &Shift{Name: "Afternoon", StartTime: "16:00", EndTime: "00:00"}

	if s.IsOvernight() {
		t.Error("IsOvernight() says a 16:00-00:00 shift is overnight; it is not, and the " +
			"test exists so that stays true")
	}
	start, end, ok := s.Window(day)
	if !ok {
		t.Fatal("Window refused a valid shift")
	}
	if end.Hour() != 0 || end.Minute() != 0 || end.Day() != 11 {
		t.Errorf("end = %s, want 00:00 on the 11th", end.Format("2006-01-02 15:04 MST"))
	}
	if start.Hour() != 16 || start.Day() != 10 {
		t.Errorf("start = %s, want 16:00 on the 10th", start.Format("2006-01-02 15:04 MST"))
	}
}

// Half-open [start, end). "Is 16:00 on an 08:00-16:00 shift on or off" has no defensible
// answer that is not written down somewhere, so it is written down here.
func TestOnShiftAtIsHalfOpen(t *testing.T) {
	day := at(2026, time.October, 10, 0, 0)
	for _, tc := range []struct {
		name  string
		shift Shift
		now   time.Time
		want  bool
	}{
		{"morning, just before", Shift{StartTime: "08:00", EndTime: "16:00"}, at(2026, time.October, 10, 7, 59), false},
		{"morning, at start", Shift{StartTime: "08:00", EndTime: "16:00"}, at(2026, time.October, 10, 8, 0), true},
		{"morning, at end", Shift{StartTime: "08:00", EndTime: "16:00"}, at(2026, time.October, 10, 16, 0), false},
		{"afternoon, last minute", Shift{StartTime: "16:00", EndTime: "00:00"}, at(2026, time.October, 10, 23, 59), true},
		{"afternoon, AT midnight", Shift{StartTime: "16:00", EndTime: "00:00"}, at(2026, time.October, 11, 0, 0), false},
		{"afternoon, past midnight", Shift{StartTime: "16:00", EndTime: "00:00"}, at(2026, time.October, 11, 0, 1), false},
		{"night, at start", Shift{StartTime: "22:00", EndTime: "06:00"}, at(2026, time.October, 10, 22, 0), true},
		{"night, past midnight", Shift{StartTime: "22:00", EndTime: "06:00"}, at(2026, time.October, 11, 3, 0), true},
		{"night, at end", Shift{StartTime: "22:00", EndTime: "06:00"}, at(2026, time.October, 11, 6, 0), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.shift.OnShiftAt(tc.now, day); got != tc.want {
				t.Errorf("OnShiftAt = %v, want %v", got, tc.want)
			}
		})
	}
}

// A start equal to the end is refused rather than read as a 24-hour shift. Both
// ValidateShift and the shifts_distinct_times CHECK reject one, so reaching it means the
// CHECK was dropped -- and "always on" is the worst possible reading of a corrupt row.
func TestWindowRefusesAZeroLengthShift(t *testing.T) {
	s := &Shift{StartTime: "08:00", EndTime: "08:00"}
	if _, _, ok := s.Window(at(2026, time.October, 10, 0, 0)); ok {
		t.Error("a shift whose start equals its end produced a window; it must be refused")
	}
}

func TestWindowRefusesUnparseableTimes(t *testing.T) {
	for _, s := range []Shift{
		{StartTime: "8:00", EndTime: "16:00"},
		{StartTime: "08:00", EndTime: ""},
		{StartTime: "24:00", EndTime: "06:00"},
	} {
		if _, _, ok := s.Window(at(2026, time.October, 10, 0, 0)); ok {
			t.Errorf("Window accepted %q-%q", s.StartTime, s.EndTime)
		}
	}
}

// THE BUG THIS PROJECT ALREADY SHIPPED ONCE.
//
// data.DateOnly stamps the LOCAL calendar date at 00:00 UTC, which is right for a value
// bound to a DATE column and wrong for anything that reads hours out of it. Building the
// window on DateOnly put this hotel's 16:00 shift at 16:00 UTC -- 15:00 here -- so
// somebody was reported on shift an hour early and stayed on an hour late. It was caught
// by reading the live payload, not by any test, which is why this one exists.
func TestLocalDayIsNotDateOnly(t *testing.T) {
	now := at(2026, time.October, 10, 16, 0)

	// The invariant, and the only part that is zone-independent: the two helpers differ
	// in their ZONE, which is the entire reason both exist. An earlier version also
	// asserted that the resulting shift windows differ, which is only true on a host
	// that is not on UTC -- so it passed or failed depending on where the suite ran.
	// A test that depends on the machine's timezone is not a test.
	if got := localDay(now).Location(); got != time.Local {
		t.Errorf("localDay is in %v, want time.Local", got)
	}
	if got := DateOnly(now).Location(); got != time.UTC {
		t.Errorf("DateOnly is in %v, want time.UTC; if this ever changes localDay may be "+
			"redundant", got)
	}
	if h, m, _ := localDay(now).Clock(); h != 0 || m != 0 {
		t.Errorf("localDay clock = %02d:%02d, want midnight", h, m)
	}

	// The behaviour that matters, and which IS zone-independent: a shift named 16:00
	// starts at 16:00 on whatever clock built it.
	s := &Shift{StartTime: "16:00", EndTime: "22:00"}
	start, _, ok := s.Window(localDay(now))
	if !ok {
		t.Fatal("Window refused a valid shift")
	}
	if sh, _, _ := start.Clock(); sh != 16 {
		t.Errorf("a 16:00 shift starts at %02d:00 wall clock; the offset leaked in", sh)
	}
	if start.Year() != 2026 || start.Month() != time.October || start.Day() != 10 {
		t.Errorf("start date = %s, want 2026-10-10", start.Format("2006-01-02"))
	}
}

// The order is what the dashboard renders, so it has to be TOTAL -- deterministic
// across identical inputs. users.created_at is timestamp(0), so same-second ties are the
// common case, which is why the UserID tail exists.
func TestSortTodayMembersIsTotal(t *testing.T) {
	start := at(2026, time.October, 10, 8, 0)
	end := at(2026, time.October, 10, 16, 0)
	// Three members identical in every visible respect except id: same status, same
	// shift, same start instant. Nothing but the tiebreak can order them, so anything
	// unstable shows up as a different sequence across runs.
	fresh := func() []*TodayMember {
		out := []*TodayMember{}
		for _, id := range []int64{7, 3, 9} {
			s, e := start, end
			out = append(out, &TodayMember{
				UserID: id, Name: "Same", Status: TodayWorkingNow,
				Shift:      &Shift{Name: "Morning", StartTime: "08:00", EndTime: "16:00"},
				ShiftStart: &s, ShiftEnd: &e,
			})
		}
		return out
	}

	probe := fresh()
	sortTodayMembers(probe)
	want := ids(probe)
	if want != "3,7,9," {
		t.Errorf("ties broke to [%s], want [3,7,9,]; the UserID tail is what makes the "+
			"order total", want)
	}
	for run := 0; run < 50; run++ {
		in := fresh()
		sortTodayMembers(in)
		if got := ids(in); got != want {
			t.Fatalf("run %d produced [%s], want [%s]", run, got, want)
		}
	}
}

// The groups come out in the order the dashboard renders them, which is the reason
// sortTodayMembers exists rather than leaving the order to the caller.
func TestSortTodayMembersOrdersGroupsThenShiftStart(t *testing.T) {
	at8 := at(2026, time.October, 10, 8, 0)
	at16 := at(2026, time.October, 10, 16, 0)
	at19 := at(2026, time.October, 10, 19, 0)
	at12 := at(2026, time.October, 10, 12, 0)

	m := []*TodayMember{
		{UserID: 1, Name: "finished", Status: TodayFinished, ShiftStart: &at8},
		{UserID: 2, Name: "coming late", Status: TodayComingSoon, ShiftStart: &at19},
		{UserID: 3, Name: "coming early", Status: TodayComingSoon, ShiftStart: &at16},
		{UserID: 4, Name: "working second", Status: TodayWorkingNow, ShiftStart: &at12},
		{UserID: 5, Name: "working first", Status: TodayWorkingNow, ShiftStart: &at8},
	}
	sortTodayMembers(m)
	got := ""
	for _, x := range m {
		got += x.Name + " | "
	}
	want := "working first | working second | coming early | coming late | finished | "
	if got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func ids(m []*TodayMember) string {
	out := ""
	for _, x := range m {
		out += itoa(x.UserID) + ","
	}
	return out
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
