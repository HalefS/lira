package data

import (
	"testing"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// The window is a run of seven calendar days, so it has to stay seven days long
// even when one of them is a daylight-saving change. Adding 7*24h to a timestamp
// would give six or eight here; AddDate on a date is what keeps it honest.
func TestLCUWindowIsSevenConsecutiveDays(t *testing.T) {
	tests := []struct {
		name  string
		start time.Time
		first string
		last  string
	}{
		{"ordinary week", date(2026, 9, 30), "2026-09-30", "2026-10-06"},
		{"spans a month end", date(2026, 1, 28), "2026-01-28", "2026-02-03"},
		// Europe/Lisbon springs forward on 29 March 2026 and falls back on 25
		// October 2026, so both of these windows contain a 23- and 25-hour day.
		{"spans spring forward", date(2026, 3, 27), "2026-03-27", "2026-04-02"},
		{"spans fall back", date(2026, 10, 22), "2026-10-22", "2026-10-28"},
		{"leap day inside", date(2026, 2, 25), "2026-02-25", "2026-03-03"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			days, endsOn := LCUWindow(tc.start)

			if len(days) != LCUTestWindowDays {
				t.Fatalf("got %d days, want %d", len(days), LCUTestWindowDays)
			}
			if got := days[0].Format("2006-01-02"); got != tc.first {
				t.Errorf("first day = %s, want %s", got, tc.first)
			}
			if got := endsOn.Format("2006-01-02"); got != tc.last {
				t.Errorf("ends_on = %s, want %s", got, tc.last)
			}
			// The last day of the window has to be the same day as ends_on, and
			// every step in between has to be exactly one day.
			if got := days[len(days)-1].Format("2006-01-02"); got != tc.last {
				t.Errorf("last day = %s, want %s (same as ends_on)", got, tc.last)
			}
			for i := 1; i < len(days); i++ {
				// Both are UTC midnights, so a one-day step is exactly 24h here.
				// If the window had been built by adding 7*24h to a timestamp, a
				// DST day would make this 23 or 25.
				if got := days[i].Sub(days[i-1]).Hours() / 24; got != 1 {
					t.Errorf("step %d advanced %v days, want 1", i, got)
				}
			}
		})
	}
}

// A time-of-day on the start date must not shift the window: someone adding a
// unit at 23:50 still gets the same seven days as someone adding it at 00:01.
func TestLCUWindowIgnoresTimeOfDay(t *testing.T) {
	morning := time.Date(2026, 9, 30, 0, 1, 0, 0, time.UTC)
	lateNight := time.Date(2026, 9, 30, 23, 50, 0, 0, time.UTC)

	_, endsMorning := LCUWindow(morning)
	_, endsLate := LCUWindow(lateNight)

	if !endsMorning.Equal(endsLate) {
		t.Errorf("ends_on differs by time of day: %v vs %v", endsMorning, endsLate)
	}
}

// A date column comes back from Postgres as midnight UTC. Comparing that against
// a local "today" must not report a different day, which is the bug that made a
// window look like it started a day early anywhere west of Greenwich.
func TestDateOnlyNormalisesToUTCMidnight(t *testing.T) {
	local := time.Date(2026, 9, 30, 23, 15, 0, 0, time.FixedZone("X", -3600))
	got := DateOnly(local)

	if got.Hour() != 0 || got.Minute() != 0 || got.Second() != 0 {
		t.Errorf("DateOnly kept a clock time: %v", got)
	}
	if got.Format("2006-01-02") != "2026-09-30" {
		t.Errorf("DateOnly moved the calendar day: got %s", got.Format("2006-01-02"))
	}
}

// A day with no result must still occupy its slot, so the window always renders
// as seven cells and a gap is visible rather than silently closing up.
func TestAttachFillsEveryDayOfTheWindow(t *testing.T) {
	unit := &LCUUnit{ID: 1, StartsOn: date(2026, 9, 30)}
	owner := int64(7)
	unit.attach([]*LCUTest{
		{UnitID: 1, Day: date(2026, 9, 30), Result: LCUResultPass, LoggedBy: &owner},
		{UnitID: 1, Day: date(2026, 10, 1), Result: LCUResultFail, LoggedBy: &owner},
		// A result that fell outside the window must be ignored, not appended.
		{UnitID: 1, Day: date(2026, 10, 9), Result: LCUResultPass, LoggedBy: &owner},
	}, map[int64]string{7: "ana"})

	if len(unit.Days) != LCUTestWindowDays {
		t.Fatalf("got %d day cells, want %d", len(unit.Days), LCUTestWindowDays)
	}
	if unit.Passes != 1 || unit.Fails != 1 {
		t.Errorf("tally = %d pass / %d fail, want 1/1", unit.Passes, unit.Fails)
	}
	if unit.Days[0].Result != LCUResultPass {
		t.Errorf("day 1 result = %q, want pass", unit.Days[0].Result)
	}
	if unit.Days[1].Result != LCUResultFail {
		t.Errorf("day 2 result = %q, want fail", unit.Days[1].Result)
	}
	for i := 2; i < LCUTestWindowDays; i++ {
		if unit.Days[i].Result != "" {
			t.Errorf("day %d has result %q, want untested", i+1, unit.Days[i].Result)
		}
	}
	if unit.Days[0].LoggedByName != "ana" {
		t.Errorf("logged_by_name = %q, want 'ana'", unit.Days[0].LoggedByName)
	}
}

func TestValidateLCUSerial(t *testing.T) {
	cases := []struct {
		name    string
		serial  string
		wantErr bool
	}{
		{"plain serial", "LCU-88231", false},
		{"surrounded by spaces is fine", "  LCU-88231  ", false},
		{"empty is rejected", "", true},
		{"only whitespace is rejected", "   ", true},
		{"at the limit", repeat("X", MaxLCUSerialLength), false},
		{"over the limit", repeat("X", MaxLCUSerialLength+1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := validator.New()
			ValidateLCUSerial(v, tc.serial)
			if got := !v.Valid(); got != tc.wantErr {
				t.Errorf("valid = %v, want %v (errors: %v)", !got, !tc.wantErr, v.Errors)
			}
		})
	}
}

func TestValidateLCUResult(t *testing.T) {
	for _, ok := range []string{LCUResultPass, LCUResultFail} {
		v := validator.New()
		ValidateLCUResult(v, ok)
		if !v.Valid() {
			t.Errorf("%q was rejected: %v", ok, v.Errors)
		}
	}
	for _, bad := range []string{"", "PASS", "passed", "green", "1"} {
		v := validator.New()
		ValidateLCUResult(v, bad)
		if v.Valid() {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
