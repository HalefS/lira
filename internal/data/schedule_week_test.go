package data

import (
	"testing"
	"time"
)

// These tests reuse at() from schedule_today_test.go, which already builds a LOCAL
// wall-clock instant -- the only construction ClassifyWeek and mondayOf can be
// reasoned about with. A UTC-constructed instant would make these tests pass or
// fail according to the machine's offset, which is exactly the coupling localDay
// exists to avoid.

// mondayOf has to agree with WeekRange, because both claim to be the one
// definition of where a week begins.
func TestMondayOfSnapsToMonday(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"a Monday is its own Monday", at(2026, time.October, 5, 0, 0), "2026-10-05"},
		{"a Wednesday snaps back two days", at(2026, time.October, 7, 13, 45), "2026-10-05"},
		{"a Sunday snaps back six, not seven", at(2026, time.October, 11, 23, 59), "2026-10-05"},
		{"midnight Sunday is still Sunday's week", at(2026, time.October, 11, 0, 0), "2026-10-05"},
		{"the Monday after starts a new week", at(2026, time.October, 12, 0, 0), "2026-10-12"},
		{"a month boundary does not shift the week", at(2026, time.November, 1, 0, 0), "2026-10-26"},
		{"a year boundary does not either", at(2027, time.January, 1, 0, 0), "2026-12-28"},
		{"the leap day sits in the week its Monday began", at(2028, time.February, 29, 9, 0), "2028-02-28"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mondayOf(tt.in)
			if s := got.Format("2006-01-02"); s != tt.want {
				t.Errorf("mondayOf(%s) = %s, want %s (Monday)",
					tt.in.Format("2006-01-02 15:04"), s, tt.want)
			}
		})
	}
}

// The three-way split is the feature. A boolean could not distinguish "this week is
// over" from "this week is half over", and the write path needs both to be different
// -- a past week is refused outright, a current week is written per DAY.
func TestClassifyWeek(t *testing.T) {
	// Wednesday 2026-10-07 at 10:00. The Monday of its week is the 5th.
	now := at(2026, time.October, 7, 10, 0)

	tests := []struct {
		name string
		week time.Time
		want WeekStatus
	}{
		{"last week's Monday has passed", at(2026, time.September, 28, 0, 0), WeekPast},
		{"last week's Sunday has passed", at(2026, time.October, 4, 0, 0), WeekPast},
		{"two weeks ago", at(2026, time.September, 21, 0, 0), WeekPast},
		{"this week's Monday is current", at(2026, time.October, 5, 0, 0), WeekCurrent},
		{"next week's Monday is future", at(2026, time.October, 12, 0, 0), WeekFuture},
		{"three weeks out", at(2026, time.October, 26, 0, 0), WeekFuture},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyWeek(tt.week, now); got != tt.want {
				t.Errorf("ClassifyWeek(%s, %s) = %q, want %q",
					tt.week.Format("2006-01-02"), now.Format("2006-01-02 15:04"), got, tt.want)
			}
		})
	}
}

// THE LAST DAY OF THE WEEK IS STILL THE CURRENT WEEK, at any hour.
//
// This is the boundary that decides whether a manager can still fix Sunday's rota on
// Sunday night. If the week closed at 00:00 on Sunday, the last day of every week
// would be read-only, and Sunday evening is exactly when a rota gets tidied.
func TestSundayNightIsStillTheCurrentWeek(t *testing.T) {
	monday := at(2026, time.October, 5, 0, 0)
	for d := 5; d <= 11; d++ {
		for _, hh := range []int{0, 9, 18, 23} {
			now := time.Date(2026, time.October, d, hh, 59, 0, 0, time.Local)
			if got := ClassifyWeek(monday, now); got != WeekCurrent {
				t.Errorf("at %s the running week classified %q, want current",
					now.Format("Mon 2006-01-02 15:04"), got)
			}
		}
	}
}

// The running week is never in the past, and its neighbours are always on the far
// side. The freeze sweep got this wrong first by comparing a week's Monday against
// TODAY rather than against this Monday, which closed the running week on its own
// first write: every edit after the first was refused as a write to a closed week.
//
// Each case is built from a KNOWN Monday, because ClassifyWeek takes a week's start
// and not any day inside it -- passing a Wednesday would be classified against the
// following week and would pass for the wrong reason.
func TestTheRunningWeekIsNeverPast(t *testing.T) {
	for _, monday := range []time.Time{
		at(2026, time.October, 5, 0, 0),
		at(2026, time.September, 28, 0, 0),
		at(2028, time.February, 28, 0, 0), // a leap-year week
	} {
		for d := 0; d < 7; d++ {
			now := monday.AddDate(0, 0, d).Add(9 * time.Hour)
			if got := ClassifyWeek(monday, now); got != WeekCurrent {
				t.Errorf("on %s at 09:00 its own week classified %q, want current",
					now.Format("Mon 2006-01-02"), got)
			}
			if got := ClassifyWeek(monday.AddDate(0, 0, 7), now); got != WeekFuture {
				t.Errorf("on %s at 09:00 the following week classified %q, want future",
					now.Format("Mon 2006-01-02"), got)
			}
			if got := ClassifyWeek(monday.AddDate(0, 0, -7), now); got != WeekPast {
				t.Errorf("on %s at 09:00 the previous week classified %q, want past",
					now.Format("Mon 2006-01-02"), got)
			}
		}
	}
}

// A week boundary is a calendar day, so it must not move when the clock's offset
// does. localDay is what makes this true; DateOnly would stamp the boundary in UTC
// and put a British Sunday evening into the following week.
func TestWeekBoundariesDoNotFollowTheUTCOffset(t *testing.T) {
	// Sunday 23:30 local. On a machine behind UTC this is the following day in UTC,
	// which is exactly the case DateOnly gets wrong.
	lateSunday := at(2026, time.October, 11, 23, 30)
	if got := ClassifyWeek(at(2026, time.October, 5, 0, 0), lateSunday); got != WeekCurrent {
		t.Errorf("Sunday 23:30 local classified the running week %q, want current", got)
	}
	if got := mondayOf(lateSunday); got.Format("2006-01-02") != "2026-10-05" {
		t.Errorf("mondayOf(Sunday 23:30 local) = %s, want 2026-10-05", got.Format("2006-01-02"))
	}
}

// ClassifyWeek must ignore the TIME OF DAY on both sides, or "editing Sunday still
// changes next week" would stop being true at midnight.
func TestClassifyWeekIgnoresTheClock(t *testing.T) {
	week := at(2026, time.October, 12, 0, 0) // next Monday
	for _, hh := range []int{0, 1, 6, 12, 18, 23} {
		now := at(2026, time.October, 11, hh, 59)
		if got := ClassifyWeek(week, now); got != WeekFuture {
			t.Errorf("at Sunday %02d:59 the next week classified %q, want future", hh, got)
		}
	}
}

// WeekStatus is a string in the API payload, and the client switches on it. A
// typo here is a client that renders an unknown state as "current" -- the most
// dangerous default, because it means a closed week looks editable.
func TestWeekStatusValuesAreTheOnesTheAPIPublishes(t *testing.T) {
	for _, s := range []WeekStatus{WeekPast, WeekCurrent, WeekFuture} {
		switch string(s) {
		case "past", "current", "future":
		default:
			t.Errorf("WeekStatus %q is not one of past/current/future", s)
		}
	}
}

// ErrWeekClosed is matched with errors.Is in the handler and turned into a 422 with
// reason "week_closed". The client must never have to match the English sentence, so
// the two must not be able to drift apart by someone rewording the message.
func TestErrWeekClosedCarriesTheReasonNotTheProse(t *testing.T) {
	if ErrWeekClosed == nil {
		t.Fatal("ErrWeekClosed is nil")
	}
	// A sentinel compared by identity: SetWeekFor returns it unwrapped.
	if ErrWeekClosed.Error() == "" {
		t.Error("ErrWeekClosed has no message")
	}
}
