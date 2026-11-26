package data

import (
	"strings"
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

// The outbox key is the one thing standing between a retried POST and a
// duplicate issue, so the accepted shape is deliberately narrow.
func TestValidClientUUID(t *testing.T) {
	valid := []string{
		"00000000-0000-0000-0000-000000000000",
		"7f9c2ba4-7d3f-4f7a-9f6e-2b1c0d9e8a71",
		"7F9C2BA4-7D3F-4F7A-9F6E-2B1C0D9E8A71",
	}
	for _, s := range valid {
		if !ValidClientUUID(s) {
			t.Errorf("%q should be accepted", s)
		}
	}

	invalid := []string{
		"",
		"not-a-uuid",
		"7f9c2ba4-7d3f-4f7a-9f6e-2b1c0d9e8a7",
		"7f9c2ba4-7d3f-4f7a-9f6e-2b1c0d9e8a710",
		"7f9c2ba4-7d3f-4f7a-9f6e-2b1c0d9e8a7g",
		"{7f9c2ba4-7d3f-4f7a-9f6e-2b1c0d9e8a71}",
	}
	for _, s := range invalid {
		if ValidClientUUID(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
}

// An issue with a third-party handover belongs to the support report, not the
// issue reports. There is no test database in this package, so this is a source
// assertion in the same spirit as issue_attachments_test.go: it fails if any of
// the four report queries loses the shared exclusion, or if that exclusion
// stops looking at the handover table.
func TestGetReportExcludesThirdPartyHandovers(t *testing.T) {
	src := lcuSource(t, "issues.go")

	helperAt := strings.Index(src, "func noHandoutClause(")
	if helperAt < 0 {
		t.Fatal("noHandoutClause not found")
	}
	helper := src[helperAt:]
	if end := strings.Index(helper, "\n}\n"); end >= 0 {
		helper = helper[:end]
	}
	if !strings.Contains(helper, "issue_support_requests") {
		t.Error("noHandoutClause no longer checks issue_support_requests")
	}
	if got := noHandoutClause("i"); !strings.Contains(got, "h.issue_id = i.id") {
		t.Errorf("noHandoutClause(%q) = %q, want it to match the issues alias", "i", got)
	}

	reportAt := strings.Index(src, "func (m IssueModel) GetReport(")
	if reportAt < 0 {
		t.Fatal("GetReport not found")
	}
	body := src[reportAt:]
	if end := strings.Index(body, "\n}\n"); end >= 0 {
		body = body[:end]
	}

	markers := []string{"summaryQ := `", "breakdownQ := `", "issueQ := `", "techQ := `"}
	for i, marker := range markers {
		at := strings.Index(body, marker)
		if at < 0 {
			t.Errorf("%s not found in GetReport", marker)
			continue
		}
		end := len(body)
		if i+1 < len(markers) {
			if next := strings.Index(body[at+len(marker):], markers[i+1]); next >= 0 {
				end = at + len(marker) + next
			}
		}
		if !strings.Contains(body[at:end], "noHandoutClause(") {
			t.Errorf("query starting at %q has no third-party handover exclusion", marker)
		}
	}
}

// The reports page says "Open / pending" while the database stores "Pending".
// Pin the mapping so a filter change cannot silently turn a request for open
// issues into an empty report, and so an unknown value is refused rather than
// being interpolated into a query.
func TestNormalizeReportStatus(t *testing.T) {
	cases := []struct {
		input       string
		wantFilter  string
		wantStored  string
		wantInvalid bool
	}{
		{"", "all", "", false},
		{"all", "all", "", false},
		{"pending", "pending", "Pending", false},
		{"open", "pending", "Pending", false},
		{"Ok", "ok", "Ok", false},
		{"resolved", "ok", "Ok", false},
		{"closed", "", "", true},
	}
	for _, tc := range cases {
		filter, stored, err := normalizeReportStatus(tc.input)
		if tc.wantInvalid {
			if err == nil {
				t.Errorf("%q: expected an error, got filter=%q stored=%q", tc.input, filter, stored)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.input, err)
			continue
		}
		if filter != tc.wantFilter || stored != tc.wantStored {
			t.Errorf("%q: got filter=%q stored=%q, want %q/%q", tc.input, filter, stored, tc.wantFilter, tc.wantStored)
		}
	}
}
