package report

import (
	"strings"
	"testing"
	"time"

	"github.com/HalefS/lira/internal/data"
)

// THE REPORTED BUG, AS A TEST
//
// A sheet was produced whose week LABEL read "05-10 a 11-10" while its seven column
// dates read 04-10 through 10-10 -- Monday from one source, Sunday from another, on
// the same page. Segunda sat over a date a day before the week the header named, and
// nothing on the page looked wrong: seven dates, seven day names, each centred over
// its column.
//
// The cause was three reads of one fact. The label came from Week.Start and
// Week.End; the column dates and the absence lookup came from Week.Days[i].Date.
// Any payload where those two disagree splits the document down the middle.
//
// So the test builds exactly that payload -- Start and End Monday-based, Days
// Sunday-based -- and asserts the sheet cannot come out inconsistent. It does not
// assert the payload is sane; the payload is not the sheet's business. It asserts the
// sheet is Monday-first and internally coherent whatever it is handed.
func skewedWeek() *data.ScheduleWeek {
	mon := time.Date(2026, 10, 5, 0, 0, 0, 0, time.Local)
	days := make([]data.WeekDay, 0, 7)
	for i := 0; i < 7; i++ {
		// One day EARLIER than the week header says.
		d := mon.AddDate(0, 0, i-1)
		days = append(days, data.WeekDay{
			Weekday: i + 1,
			Label:   []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}[i],
			Date:    data.JSONDate(d),
		})
	}
	m := &data.ScheduleMember{UserID: 1, Name: "Ana", Active: true}
	m.Days = [7]*data.Shift{attShift("08:00", "16:00", false), nil, nil, nil, nil, nil, nil}
	return &data.ScheduleWeek{
		Week:    &data.Week{Start: data.JSONDate(mon), End: data.JSONDate(mon.AddDate(0, 0, 6)), Days: days},
		Members: []*data.ScheduleMember{m},
	}
}

func TestAttendanceSheetIgnoresSkewedDayRows(t *testing.T) {
	v := newAttendanceView(skewedWeek())

	if v.WeekLabel != "05-10 a 11-10" {
		t.Errorf("week label = %q, want the Monday-to-Sunday range", v.WeekLabel)
	}
	// The column dates must follow the ANCHOR, not the payload's day rows.
	want := []string{"05-10", "06-10", "07-10", "08-10", "09-10", "10-10", "11-10"}
	for i, d := range v.Days {
		if d.Date != want[i] {
			t.Errorf("column %d is dated %q, want %q", i, d.Date, want[i])
		}
		if d.Weekday != i+1 || d.Name != attendanceWeekdays[i] {
			t.Errorf("column %d is %q weekday %d, want %q weekday %d",
				i, d.Name, d.Weekday, attendanceWeekdays[i], i+1)
		}
	}
	// And nothing anywhere may show the day the payload asked for.
	for _, forbidden := range []string{"04-10"} {
		if v.Days[0].Date == forbidden {
			t.Errorf("the first column reads %s, the day before the week", forbidden)
		}
		if strings.Contains(v.WeekLabel, forbidden) {
			t.Errorf("the week label mentions %s", forbidden)
		}
	}

	// The rendered page must not contain the skewed date in the date row either.
	h := renderOK(t, skewedWeek())
	row := ""
	if m := strings.Index(h, "<tr class=\"h-dates\""); m >= 0 {
		row = h[m:min(m+900, len(h))]
	}
	if strings.Contains(row, ">04-10<") {
		t.Errorf("the printed date row still shows 04-10:\n%s", row)
	}
	if !strings.Contains(row, ">05-10<") {
		t.Errorf("the printed date row is missing 05-10:\n%s", row)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// An anchor that is not a Monday must still produce a Monday-to-Sunday sheet. The
// rota page only ever sends a Monday, but the report endpoints accept any date and
// nothing guarantees the caller was careful.
func TestAttendanceSheetSnapsASundayAnchor(t *testing.T) {
	sunday := time.Date(2026, 10, 4, 0, 0, 0, 0, time.Local) // a Sunday
	w := &data.ScheduleWeek{
		Week:    &data.Week{Start: data.JSONDate(sunday), End: data.JSONDate(sunday.AddDate(0, 0, 6))},
		Members: []*data.ScheduleMember{{UserID: 1, Name: "Ana", Active: true}},
	}
	v := newAttendanceView(w)
	if v.WeekLabel != "05-10 a 11-10" {
		t.Errorf("a Sunday anchor produced the week %q, want 05-10 a 11-10", v.WeekLabel)
	}
	if v.Days[0].Date != "05-10" || v.Days[0].Name != "Segunda" {
		t.Errorf("first column is %q dated %q, want Segunda dated 05-10",
			v.Days[0].Name, v.Days[0].Date)
	}
	// Delivery is the Monday AFTER the printed week, whichever anchor was given.
	if v.DeliveryLabel != "12-10-26" {
		t.Errorf("delivery = %q, want 12-10-26", v.DeliveryLabel)
	}
}

// The absence lookup used to read its dates from Week.Days while the label came from
// Week.Start, so on a skewed payload an absence was matched against the wrong day --
// printed under one name and filed against another.
func TestAttendanceAbsenceLookupUsesTheAnchor(t *testing.T) {
	w := skewedWeek()
	// Cover the Monday the LABEL names. On the skewed payload that is not Days[0].
	w.Absences = []*data.AbsenceRef{attAbsence(1, data.AbsenceVacation, "2026-10-05", "2026-10-05")}

	v := newAttendanceView(w)
	row := v.Rows[0]
	if row.Cells[0].Status != "Feria" {
		t.Errorf("an absence on the Monday the label names should print on column 0, got %q",
			row.Cells[0].Status)
	}
	// And not on the Sunday the payload pointed at.
	for i, c := range row.Cells[1:] {
		if c.Status == "Feria" {
			t.Errorf("the absence leaked onto column %d, which the payload dated %s",
				i+1, w.Week.Days[i+1].Date.Time().Format("02-01"))
		}
	}
}

// Everything the page prints must describe the same week. This is the assertion the
// whole change exists for: the reported sheet had a header and a table that
// disagreed, and each was individually plausible.
func TestAttendanceEveryPrintedDateDescribesOneWeek(t *testing.T) {
	for _, w := range []*data.ScheduleWeek{attWeekPayload("2026-10-05"), skewedWeek()} {
		v := newAttendanceView(w)
		mon := mondayOf(w.Week.Start.Time())

		if got := v.FromDate; got != mon.Format(time.DateOnly) {
			t.Errorf("FromDate %q is not the Monday of the week %q", got, v.WeekLabel)
		}
		if got := v.ToDate; got != mon.AddDate(0, 0, 6).Format(time.DateOnly) {
			t.Errorf("ToDate %q is not the Sunday of the week %q", got, v.WeekLabel)
		}
		if got := v.DeliveryLabel; got != mon.AddDate(0, 0, 7).Format("02-01-06") {
			t.Errorf("Data Entrega %q is not the Monday after %q", got, v.WeekLabel)
		}
		if !strings.Contains(v.WeekLabel, v.Days[0].Date) {
			t.Errorf("the week label %q does not contain its own first column date %q",
				v.WeekLabel, v.Days[0].Date)
		}
		if !strings.Contains(v.WeekLabel, v.Days[6].Date) {
			t.Errorf("the week label %q does not contain its own last column date %q",
				v.WeekLabel, v.Days[6].Date)
		}
	}
}

func attWeekPayload(monday string) *data.ScheduleWeek {
	return &data.ScheduleWeek{Week: attWeek(monday)}
}
