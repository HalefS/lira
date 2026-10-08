package report

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HalefS/lira/internal/data"
)

// A report was shipped with its day columns headed in English -- MON, TUE -- because
// the view preferred WeekDay.Label over its own Portuguese table, and the server sets
// that label to a short English weekday. It survived every other test here because
// each of them asked whether the right DATA reached the page, not whether the right
// NAME sat above it.
//
// So these tests assert the pairing itself: for a known week, the day name and the
// date in the same column have to agree, in BOTH renderers, and Monday has to be the
// 5th. An off-by-one between the day heads and the dates cannot pass these.

func TestAttendanceDayColumnsPairNameWithDate(t *testing.T) {
	// 2026-10-05 is a Monday. The 4th is a Sunday, which is exactly what a sheet that
	// started its week on the wrong day would print above Segunda.
	w := &data.ScheduleWeek{Week: attWeek("2026-10-05")}
	v := newAttendanceView(w)

	want := []struct{ name, date string }{
		{"Segunda", "05-10"},
		{"Terça", "06-10"},
		{"Quarta", "07-10"},
		{"Quinta", "08-10"},
		{"Sexta", "09-10"},
		{"Sábado", "10-10"},
		{"Domingo", "11-10"},
	}
	if len(v.Days) != 7 {
		t.Fatalf("expected 7 day columns, got %d", len(v.Days))
	}
	for i, wnt := range want {
		got := v.Days[i]
		if got.Name != wnt.name {
			t.Errorf("column %d is headed %q, want %q", i, got.Name, wnt.name)
		}
		if got.Date != wnt.date {
			t.Errorf("column %d is headed %q and dated %q; Monday must be the 5th, not the 4th",
				i, got.Name, got.Date)
		}
		if got.Weekday != i+1 {
			t.Errorf("column %d has ISO weekday %d, want %d", i, got.Weekday, i+1)
		}
	}
}

// The weekday NAME comes from the ISO weekday, never from the payload. WeekDay.Label
// is the server's short label and it is English; a sheet that prefers it heads its
// columns MON and TUE above a Portuguese document.
func TestAttendanceDayNamesIgnoreTheEnglishServerLabel(t *testing.T) {
	w := &data.ScheduleWeek{Week: attWeek("2026-10-05")}
	// Poison the labels the way the live payload does.
	english := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	for i := range w.Week.Days {
		w.Week.Days[i].Label = english[i]
	}
	for i, d := range newAttendanceView(w).Days {
		if d.Name == english[i] {
			t.Errorf("column %d took the server label %q; a Portuguese sheet must not", i, d.Name)
		}
		if d.Name != attendanceWeekdays[i] {
			t.Errorf("column %d is %q, want %q", i, d.Name, attendanceWeekdays[i])
		}
	}
	// The rendered page must carry the Portuguese names and none of the English.
	h := renderOK(t, w)
	for _, en := range english {
		if strings.Contains(h, ">"+en+"<") {
			t.Errorf("the sheet prints the English weekday %q", en)
		}
	}
	for _, pt := range attendanceWeekdays {
		if !strings.Contains(h, ">"+pt+"<") {
			t.Errorf("the sheet is missing the Portuguese weekday %q", pt)
		}
	}
}

// The two renderers must not drift. They build their date labels from DIFFERENT
// places -- the PDF from Week.Days[i].Date, the spreadsheet from Week.Start plus i --
// so a disagreement between the two is invisible until somebody compares them.
func TestAttendanceRenderersAgreeOnEveryDate(t *testing.T) {
	for _, monday := range []string{"2026-10-05", "2026-01-05", "2026-03-30", "2027-12-27"} {
		w := &data.ScheduleWeek{
			Week: attWeek(monday),
			Members: []*data.ScheduleMember{
				{UserID: 1, Name: "Ana", Active: true},
			},
		}
		v := newAttendanceView(w)

		_, doc := renderXLSX(t, w)
		pool := doc.pool(t)

		for i, d := range v.Days {
			ref := string(xlsxDayCols[i*2]) + "9"
			raw := doc.cell(t, xlsxSheet, ref)
			serial, err := strconv.Atoi(raw)
			if err != nil {
				t.Fatalf("%s %s: date cell is %q, not a serial", monday, ref, raw)
			}
			epoch := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
			got := epoch.AddDate(0, 0, serial).Format("02-01")
			if got != d.Date {
				t.Errorf("%s column %d: the spreadsheet says %s and the PDF says %s",
					monday, i, got, d.Date)
			}
			// And the Monday of that week is the Monday the week header names.
			start, _ := time.ParseInLocation("2006-01-02", monday, time.Local)
			byStart := start.AddDate(0, 0, i).Format("02-01")
			if got != byStart {
				t.Errorf("%s column %d: %s is not Monday %d of the week (%s)",
					monday, i, got, i, byStart)
			}
		}
		_ = pool
	}
}

// The first column is Monday, always. This is the whole bug in one assertion: the
// workbook template own later weeks run Sunday-to-Saturday, and a report that started
// its columns there would put the Sunday date above Segunda and the Monday date above
// Terca -- a sheet that is wrong by a whole day and looks otherwise perfect.
func TestAttendanceFirstColumnIsMonday(t *testing.T) {
	for _, monday := range []string{"2026-10-05", "2026-01-05", "2026-03-30"} {
		start, _ := time.ParseInLocation("2006-01-02", monday, time.Local)
		if start.Weekday() != time.Monday {
			t.Fatalf("fixture %s is not a Monday", monday)
		}
		v := newAttendanceView(&data.ScheduleWeek{Week: attWeek(monday)})
		if v.Days[0].Weekday != 1 {
			t.Errorf("%s: column 0 is ISO weekday %d, want 1 (Monday)", monday, v.Days[0].Weekday)
		}
		if v.Days[0].Name != "Segunda" {
			t.Errorf("%s: column 0 is headed %q, want Segunda", monday, v.Days[0].Name)
		}
		// The day before Monday is Sunday the 4th-ish, and must appear nowhere.
		if strings.Contains(v.Days[0].Date, "04") {
			t.Errorf("%s: column 0 reads %q, which is the day before Monday", monday, v.Days[0].Date)
		}
	}
}

// The printed page puts the day head and its date in the same visual row pair, in
// order, so a template that emitted the dates row before the heads, or twice, would
// be caught.
func TestAttendancePDFPrintsHeadsBeforeDates(t *testing.T) {
	m := &data.ScheduleMember{UserID: 1, Name: "Ana", Active: true}
	h := renderOK(t, &data.ScheduleWeek{
		Week:    attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{m},
	})
	rows := regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`).FindAllStringSubmatch(h, -1)
	var head, dates, hora int
	for _, r := range rows {
		switch {
		case strings.Contains(r[1], "Segunda"):
			head++
		case strings.Contains(r[1], "Entrada"):
			hora++
		case strings.Contains(r[1], "Datas"):
			dates++
		}
	}
	if head != 1 || hora != 1 || dates != 1 {
		t.Fatalf("expected one Dia row, one Hora row and one Datas row; got %d, %d, %d",
			head, hora, dates)
	}
	var order []string
	for _, r := range rows {
		if strings.Contains(r[1], "Segunda") {
			order = append(order, "dia")
		}
		if strings.Contains(r[1], "Entrada") {
			order = append(order, "hora")
		}
		if strings.Contains(r[1], "Datas") {
			order = append(order, "datas")
		}
	}
	if strings.Join(order, ",") != "dia,hora,datas" {
		t.Errorf("header rows are out of order: %v", order)
	}
}
