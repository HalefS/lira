package data

import (
	"testing"
	"time"
)

func d(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

func TestClampMaintenanceIntervalDays(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero is clamped up to a week", 0, MinMaintenanceIntervalDays},
		{"negative is clamped up to a week", -30, MinMaintenanceIntervalDays},
		{"a week is left alone", 7, 7},
		{"a month is left alone", 30, 30},
		{"a year is left alone", 365, 365},
		{"two years is the ceiling", 730, MaxMaintenanceIntervalDays},
		{"beyond two years is clamped", 5000, MaxMaintenanceIntervalDays},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClampMaintenanceIntervalDays(tc.in); got != tc.want {
				t.Errorf("ClampMaintenanceIntervalDays(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// A device nobody has ever checked is due an interval from when it was added, and
// not from the epoch or from now -- otherwise adding a device and immediately
// checking it would push the first due date out twice.
func TestMaintenanceDueOnNeverChecked(t *testing.T) {
	created := d(2026, time.March, 1)
	got := MaintenanceDueOn(nil, created, 30)
	if want := d(2026, time.March, 31); !got.Equal(want) {
		t.Errorf("MaintenanceDueOn(nil, 2026-03-01, 30) = %v, want %v", got, want)
	}
}

// The load-bearing case: the due date runs from the last actual check, so
// recording a check is the only thing that moves it. This is what stops the
// queue being satisfied without the work being done.
func TestMaintenanceDueOnRunsFromLastCheck(t *testing.T) {
	lastDone := d(2026, time.June, 10)
	created := d(2026, time.January, 1)

	got := MaintenanceDueOn(&lastDone, created, 90)
	if want := d(2026, time.September, 8); !got.Equal(want) {
		t.Errorf("due = %v, want %v", got, want)
	}

	// The creation date must not leak in when there is a last check.
	if got.Equal(MaintenanceDueOn(nil, created, 90)) {
		t.Errorf("last check was ignored: due came out as %v, same as never-checked", got)
	}
}

// The time of day on the last check must not shift the date. An interval is a
// count of days, and carrying a clock time across would make a check recorded at
// 23:00 land a day early or late depending on when it was filed.
func TestMaintenanceDueOnIgnoresTimeOfDay(t *testing.T) {
	morning := time.Date(2026, time.June, 10, 8, 0, 0, 0, time.UTC)
	night := time.Date(2026, time.June, 10, 23, 30, 0, 0, time.UTC)

	if a, b := MaintenanceDueOn(&morning, d(2026, time.January, 1), 30),
		MaintenanceDueOn(&night, d(2026, time.January, 1), 30); !a.Equal(b) {
		t.Errorf("time of day leaked: morning gave %v, night gave %v", a, b)
	}
}

// A cadence that was somehow stored out of range must not produce an absurd due
// date. Clamping here rather than trusting the column means a hand-edited row
// still produces a schedule somebody can act on.
func TestMaintenanceDueOnClampsBadInterval(t *testing.T) {
	created := d(2026, time.March, 1)
	got := MaintenanceDueOn(nil, created, 0)
	if want := d(2026, time.March, 8); !got.Equal(want) {
		t.Errorf("a zero interval gave %v, want %v", got, want)
	}
}

func TestMaintenanceDueState(t *testing.T) {
	today := d(2026, time.October, 2)
	tests := []struct {
		name string
		due  time.Time
		want string
	}{
		{"far future is merely scheduled", d(2026, time.December, 25), DueScheduled},
		{"just outside the window is scheduled", d(2026, time.October, 17), DueScheduled},
		{"the far edge of the window counts as soon", d(2026, time.October, 16), DueSoon},
		{"the day after tomorrow is soon", d(2026, time.October, 4), DueSoon},
		{"due today is soon, not overdue", d(2026, time.October, 2), DueSoon},
		{"yesterday is overdue", d(2026, time.October, 1), DueOverdue},
		{"long overdue is overdue", d(2025, time.February, 1), DueOverdue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaintenanceDueState(tc.due, today, true); got != tc.want {
				t.Errorf("MaintenanceDueState(%v, today, true) = %q, want %q", tc.due, got, tc.want)
			}
		})
	}
}

// A retired device is out of the queue, so it can never be reported overdue
// however long ago it was last looked at.
func TestMaintenanceDueStateRetiredIsNeverOverdue(t *testing.T) {
	today := d(2026, time.October, 2)
	longAgo := d(2020, time.January, 1)
	if got := MaintenanceDueState(longAgo, today, false); got != DueScheduled {
		t.Errorf("a retired device came out as %q, want %q", got, DueScheduled)
	}
}

// The time of day on either date must not decide whether something is overdue.
func TestMaintenanceDueStateIgnoresTimeOfDay(t *testing.T) {
	today := d(2026, time.October, 2)
	lateToday := time.Date(2026, time.October, 2, 23, 59, 0, 0, time.UTC)
	if got := MaintenanceDueState(lateToday, today, true); got == DueOverdue {
		t.Errorf("a due date of late today was called overdue")
	}
}

func TestMaintenanceDaysOverdue(t *testing.T) {
	today := d(2026, time.October, 2)
	tests := []struct {
		name string
		due  time.Time
		want int
	}{
		{"not yet due is zero, never negative", d(2026, time.October, 20), 0},
		{"due today is zero", d(2026, time.October, 2), 0},
		{"one day late", d(2026, time.October, 1), 1},
		{"nine days late", d(2026, time.September, 23), 9},
		// 2192 days across 2020-2025 (two leap years) plus 274 to 2 October.
		{"years late", d(2020, time.January, 1), 2466},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := maintenanceDaysOverdue(tc.due, today); got != tc.want {
				t.Errorf("maintenanceDaysOverdue(%v) = %d, want %d", tc.due, got, tc.want)
			}
		})
	}
}

func TestDefaultIntervalDays(t *testing.T) {
	if got := DefaultIntervalDays(EquipmentPrinter); got != DefaultPrinterIntervalDays {
		t.Errorf("printer default = %d, want %d", got, DefaultPrinterIntervalDays)
	}
	if got := DefaultIntervalDays(EquipmentPhone); got != DefaultPhoneIntervalDays {
		t.Errorf("phone default = %d, want %d", got, DefaultPhoneIntervalDays)
	}
	// Anything unrecognised falls back to the printer round rather than to
	// something arbitrary like zero.
	if got := DefaultIntervalDays(""); got != DefaultPrinterIntervalDays {
		t.Errorf("unknown type default = %d, want %d", got, DefaultPrinterIntervalDays)
	}
}
