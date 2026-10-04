package data

import (
	"testing"
	"time"
)

func calDay(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

// A pending issue stays on today's list until it is solved, but only today's list
// behaves that way. Every other date has to stay the day it was, or the history
// page stops being history.
func TestCarryPendingOntoToday(t *testing.T) {
	today := calDay(2026, time.October, 4)

	tests := []struct {
		name string
		date string
		want bool
	}{
		{"today carries pending", "2026-10-04", true},
		{"yesterday is yesterday alone", "2026-10-03", false},
		{"tomorrow is not today", "2026-10-05", false},
		{"last month is that month alone", "2026-09-04", false},
		{"last year is that year alone", "2025-10-04", false},
		{"no date means every date, so nothing is carried", "", false},
		// A malformed or differently formatted date is not today either. It is
		// passed through to SQL as before, where it matches no rows, rather than
		// being quietly reinterpreted as today.
		{"unparseable date is not today", "not-a-date", false},
		{"wrong format is not today", "04/10/2026", false},
		{"timestamp instead of a date is not today", "2026-10-04T00:00:00Z", false},
		{"untrimmed whitespace is not a match", " 2026-10-04 ", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := carryPendingOntoToday(tc.date, today); got != tc.want {
				t.Errorf("carryPendingOntoToday(%q, today) = %v, want %v", tc.date, got, tc.want)
			}
		})
	}
}

// The hour inside "today" must not change the answer. Today() is built at
// midnight, so a test that pinned the time of day would be testing the clock
// rather than the rule.
func TestCarryPendingOntoTodayIgnoresTimeOfDay(t *testing.T) {
	midnight := calDay(2026, time.October, 4)
	lateEvening := time.Date(2026, time.October, 4, 23, 59, 59, 0, time.UTC)

	for _, now := range []time.Time{midnight, lateEvening} {
		if !carryPendingOntoToday("2026-10-04", now) {
			t.Errorf("carryPendingOntoToday with today at %v = false, want true", now)
		}
		if carryPendingOntoToday("2026-10-05", now) {
			t.Errorf("carryPendingOntoToday with today at %v carried tomorrow, want false", now)
		}
	}
}

// An issue logged on one day and left pending has to keep appearing on each
// following day's list, which is the behaviour the date filter exists to give.
// This walks the days so a regression says which one broke.
func TestCarryPendingOntoTodaySpansDays(t *testing.T) {
	// Day 1: logged and left pending. It is today, so it carries.
	day1 := calDay(2026, time.October, 1)
	if !carryPendingOntoToday("2026-10-01", day1) {
		t.Fatal("on the day it was logged it must be today, so pending carries")
	}

	// Days 2, 3 and 4: still pending, so still each day's list.
	for _, day := range []int{2, 3, 4} {
		now := calDay(2026, time.October, day)
		date := now.Format(time.DateOnly)
		if !carryPendingOntoToday(date, now) {
			t.Errorf("on %s a pending issue must still be carried onto today's list", date)
		}
	}

	// And once a later day is asked about, the old date is just a date. The
	// query's pending arm no longer reaches back, because it only applies when
	// the date asked for is today.
	later := calDay(2026, time.October, 5)
	if carryPendingOntoToday("2026-10-01", later) {
		t.Error("October 1st asked for on the 5th is a past date and must not carry pending")
	}
}
