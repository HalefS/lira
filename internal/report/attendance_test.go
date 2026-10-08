package report

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HalefS/lira/internal/data"
)

// A Monday, so every fixture lines up with Days[0] the way Week.Days does.
func attDay(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func attWeek(start string) *data.Week {
	monday := attDay(start)
	days := make([]data.WeekDay, 0, 7)
	for i := 0; i < 7; i++ {
		d := monday.AddDate(0, 0, i)
		days = append(days, data.WeekDay{
			Weekday: i + 1,
			Label:   attendanceWeekdays[i],
			Date:    data.JSONDate(d),
			IsToday: i == 2,
		})
	}
	return &data.Week{
		Start: data.JSONDate(monday),
		End:   data.JSONDate(monday.AddDate(0, 0, 6)),
		Days:  days,
	}
}

func attShift(start, end string, overnight bool) *data.Shift {
	return &data.Shift{Name: "Morning", StartTime: start, EndTime: end, Overnight: overnight}
}

// attNight is attShift with the name the application's night shift actually has,
// which the key prints beside the cell's times.
func attNight(start, end string) *data.Shift {
	return &data.Shift{Name: "Night", StartTime: start, EndTime: end, Overnight: true}
}

func attAbsence(user int64, kind, from, to string) *data.AbsenceRef {
	return &data.AbsenceRef{
		ID:       1,
		UserID:   user,
		Kind:     kind,
		StartsOn: data.JSONDate(attDay(from)),
		EndsOn:   data.JSONDate(attDay(to)),
	}
}

// renderOK renders and fails the test if that errors, so the rest of each test can
// read the HTML directly.
func renderOK(t *testing.T, w *data.ScheduleWeek) string {
	t.Helper()
	h, err := RenderAttendanceHTML(w)
	if err != nil {
		t.Fatalf("RenderAttendanceHTML: %v", err)
	}
	return h
}

func TestRenderAttendanceHTMLRejectsNil(t *testing.T) {
	if _, err := RenderAttendanceHTML(nil); err == nil {
		t.Error("a nil week must be an error, not an empty sheet")
	}
	// The nil-outer-struct case does not cover a payload whose header never
	// arrived, and that one would otherwise render a sheet with no columns at all.
	if _, err := RenderAttendanceHTML(&data.ScheduleWeek{}); err == nil {
		t.Error("a week with no Week header must be an error")
	}
}

// Member display names are user-supplied and land in a grid cell once per weekday.
// This is the one assertion in the file that matters most.
func TestRenderAttendanceHTMLEscapesNames(t *testing.T) {
	h := renderOK(t, &data.ScheduleWeek{
		Week: attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{
			{UserID: 1, Name: `<script>alert("x")</script>`, Role: "manager", Active: true},
			{UserID: 1, Name: `Ana & Sons <Ltd>`, Role: "technician", Active: true},
		},
	})
	if strings.Contains(h, "<script>alert") {
		t.Error("a member name was emitted unescaped")
	}
	if !strings.Contains(h, "&lt;script&gt;") {
		t.Error("the script tag should appear escaped in the output")
	}
	if strings.Contains(h, "Ana & Sons <Ltd>") {
		t.Error("an ampersand in a member name was emitted unescaped")
	}
}

// The document is Portuguese. An English word here means somebody "aligned" it with
// the other two reports, which are internal English diagnostics.
func TestAttendanceStatusWordsArePortuguese(t *testing.T) {
	h := renderOK(t, &data.ScheduleWeek{
		Week: attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{
			{UserID: 1, Name: "Todos", Role: "manager", Active: true},
		},
		Absences: []*data.AbsenceRef{
			attAbsence(1, data.AbsenceVacation, "2026-10-05", "2026-10-05"),
			attAbsence(1, data.AbsenceSick, "2026-10-06", "2026-10-06"),
			attAbsence(1, data.AbsenceTraining, "2026-10-07", "2026-10-07"),
			attAbsence(1, data.AbsenceUnavailable, "2026-10-08", "2026-10-08"),
		},
	})
	for _, want := range []string{"Feria", "Falta", "Formação", "Indisponível", "Folga"} {
		if !strings.Contains(h, ">"+want+"<") {
			t.Errorf("expected the Portuguese status word %q in the output", want)
		}
	}
	for _, banned := range []string{">HOLIDAY<", ">SICK<", ">TRAINING<", ">AWAY<", ">VACATION<"} {
		if strings.Contains(h, banned) {
			t.Errorf("the English status word %q leaked into a Portuguese document", banned)
		}
	}
	// "Feriado" is the word the Excel template has, and the application has no
	// public-holiday data behind it. Printing it would be a promise the data
	// cannot keep.
	if strings.Contains(h, ">Feriado<") {
		t.Error("Feriado must not be printed: nothing in the schema can produce it")
	}
}

// The load-bearing precedence rule. An absence beats the recurring shift for that
// day on a printed sheet, because only one thing can be rendered per cell. The
// failure is invisible otherwise: the document still looks complete.
func TestAttendanceAbsenceBeatsShift(t *testing.T) {
	m := &data.ScheduleMember{UserID: 1, Name: "Ana", Role: "technician", Active: true}
	m.Days = [7]*data.Shift{attShift("08:00", "16:00", false), nil, nil, nil, nil, nil, nil}

	h := renderOK(t, &data.ScheduleWeek{
		Week:    attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{m},
		// Covers Monday, which is also a worked day.
		Absences: []*data.AbsenceRef{attAbsence(1, data.AbsenceVacation, "2026-10-05", "2026-10-06")},
	})

	if !strings.Contains(h, ">Feria<") {
		t.Error("the covered day should print the absence word")
	}
	if strings.Contains(h, "08:00-16:00") {
		t.Error("the shift times must not print on a day an absence covers")
	}
}

// The other half of the rule above: precedence is not deletion. A week where one
// day is covered and another is not must still print the uncovered shift, or the
// row loses its rota entirely.
func TestAttendanceShiftSurvivesOnOtherDays(t *testing.T) {
	m := &data.ScheduleMember{UserID: 1, Name: "Ana", Role: "technician", Active: true}
	m.Days = [7]*data.Shift{
		attShift("08:00", "16:00", false), attShift("09:00", "17:00", false),
		nil, nil, nil, nil, nil,
	}

	h := renderOK(t, &data.ScheduleWeek{
		Week:     attWeek("2026-10-05"),
		Members:  []*data.ScheduleMember{m},
		Absences: []*data.AbsenceRef{attAbsence(1, data.AbsenceVacation, "2026-10-05", "2026-10-05")},
	})

	if strings.Contains(h, ">08:00<") {
		t.Error("the covered Monday shift should not print")
	}
	if !strings.Contains(h, ">09:00<") || !strings.Contains(h, ">17:00<") {
		t.Error("the uncovered Tuesday shift must still print, both of its times")
	}
}

// "Away all week" and "not on this rota" are different facts, and the sheet still
// tells them apart -- but now the way the WORKBOOK does, in the word in the cell
// rather than a label beside the name. An absent member is all "Feria"; one with no
// shifts is all "Folga". A sheet that printed both as "Folga" would be telling a
// manager somebody worked a day they were on holiday for, which is the one thing the
// absence feature exists to prevent.
//
// The flags are asserted on the view; the difference a reader actually sees is
// asserted on the rendered words.
func TestAttendanceAwayAllWeekIsNotDayOff(t *testing.T) {
	m := &data.ScheduleMember{UserID: 1, Name: "Ana", Role: "technician", Active: true}

	absentAllWeek := &data.ScheduleWeek{
		Week:    attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{m},
		Absences: []*data.AbsenceRef{
			attAbsence(1, data.AbsenceVacation, "2026-10-05", "2026-10-11"),
		},
	}
	v := newAttendanceView(absentAllWeek)
	if !v.Rows[0].AwayAll {
		t.Error("a member absent all seven days should be away all week")
	}
	if v.Rows[0].NotOnRota {
		t.Error("an absent member is not the same as having no rota")
	}
	if v.Rows[0].AwayWord != "Feria" {
		t.Errorf("the away-all sub-line should name the absence, got %q", v.Rows[0].AwayWord)
	}

	h := renderOK(t, absentAllWeek)
	if strings.Contains(h, "Folga") {
		t.Error("an absent member must not be printed as Folga on any day")
	}
	if n := strings.Count(h, ">Feria<"); n < 7 {
		t.Errorf("an absent member should carry the absence word on all seven days, got %d", n)
	}

	// The other half, on the page as well as the view: no shifts and no absence is
	// Folga in all seven cells, which is a different document from the one above.
	v2 := newAttendanceView(&data.ScheduleWeek{
		Week:    attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{m},
	})
	if v2.Rows[0].AwayAll {
		t.Error("a member with no shifts and no absence is not away all week")
	}
	if !v2.Rows[0].NotOnRota {
		t.Error("a member with no shifts should be flagged as not on the rota")
	}
	if h2 := renderOK(t, &data.ScheduleWeek{
		Week:    attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{m},
	}); !strings.Contains(h2, ">Folga<") {
		t.Error("a member with no shifts should print Folga, which is what the workbook does")
	}
}

// A night shift prints its two times, and the footnote stating the convention
// appears only when the week actually contains a night.
//
// There is deliberately NO "+1" marker in the cell. It was there, and at 7pt DM
// Mono it made the cell 22mm wide against a 19mm column: the text overflowed into
// the next weekday and printed "22:00-06:00 +108:00-16:00", destroying the one
// distinction the sheet exists to convey. A finish earlier than the start is plainly
// next-day on a clock sheet, and the key names Night with its times. This test is
// the guard against the marker being re-added as a well-meaning "improvement".
func TestAttendanceOvernightExplainsItselfWithoutAMarker(t *testing.T) {
	m := &data.ScheduleMember{UserID: 1, Name: "Ana", Role: "technician", Active: true}
	m.Days = [7]*data.Shift{attNight("22:00", "06:00"), nil, nil, nil, nil, nil, nil}

	w := &data.ScheduleWeek{Week: attWeek("2026-10-05"), Members: []*data.ScheduleMember{m}}
	v := newAttendanceView(w)
	if !v.HasOvernight {
		t.Error("a night shift should set HasOvernight")
	}
	if !v.Rows[0].Cells[0].Overnight {
		t.Error("the cell should still know the shift was overnight")
	}
	if v.FootNote == "" {
		t.Error("the footnote should explain the convention when a night is present")
	}

	h := renderOK(t, w)
	if !strings.Contains(h, ">22:00<") || !strings.Contains(h, ">06:00<") {
		t.Error("the night should print both its times, one under each heading")
	}
	if strings.Contains(h, "+1") {
		t.Error("the +1 marker overflows the day column; do not re-add it to the cell")
	}
	if !strings.Contains(h, "termina no dia seguinte") {
		t.Error("the footnote should explain a next-day finish")
	}
	// The workbook prints no shift names and no key, so neither does this. A reader
	// sees 22:00 in one column and 06:00 in the next, and knows it finishes after
	// midnight, which is what a clock sheet asks of them.
	// Asserted on the markup, not on the word: "Turnos" is part of the title
	// "Folha de Ponto/Turnos", so a substring check would pass for the wrong reason.
	if strings.Contains(h, `class="legend-h"`) {
		t.Error("the PDF must not add a shift key; the workbook has none")
	}

	// A week with no nights must not carry a line about nothing.
	plain := &data.ScheduleMember{UserID: 1, Name: "Bo", Role: "technician", Active: true}
	plain.Days = [7]*data.Shift{attShift("08:00", "16:00", false), nil, nil, nil, nil, nil, nil}
	h2 := renderOK(t, &data.ScheduleWeek{
		Week:    attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{plain},
	})
	if strings.Contains(h2, "termina no dia seguinte") {
		t.Error("the +1 footnote must not appear in a week with no overnight shift")
	}
}

// A missing column must be visibly absent rather than silently short.
func TestAttendanceSevenColumnsAlways(t *testing.T) {
	for _, w := range []*data.ScheduleWeek{
		{Week: attWeek("2026-10-05")},
		{Week: attWeek("2026-10-05"), Members: []*data.ScheduleMember{{UserID: 1, Name: "Ana", Active: true}}},
	} {
		v := newAttendanceView(w)
		if len(v.Days) != 7 {
			t.Errorf("expected 7 day columns, got %d", len(v.Days))
		}
		for _, r := range v.Rows {
			if len(r.Cells) != 7 {
				t.Errorf("%s: expected 7 cells, got %d", r.Name, len(r.Cells))
			}
		}
	}
}

// For a signed document this matters more than for a diagnostics report: the reader
// must be able to tell WHICH week came back empty.
func TestAttendanceEmptyWeekStillShowsItsDates(t *testing.T) {
	h := renderOK(t, &data.ScheduleWeek{Week: attWeek("2026-10-05")})
	if !strings.Contains(h, "Sem membros na rota") {
		t.Error("an empty week should say so")
	}
	if !strings.Contains(h, "05-10 a 11-10") {
		t.Error("an empty week must still print its date range")
	}
	if !strings.Contains(h, "Outubro 2026") {
		t.Error("an empty week must still print its month")
	}
}

// Hours are the minutes actually worked, absences excluded. Being told 40 h for
// somebody on holiday is the one number that makes an uneven rota invisible.
func TestAttendanceHoursExcludeAbsentDays(t *testing.T) {
	m := &data.ScheduleMember{UserID: 1, Name: "Ana", Role: "technician", Active: true}
	work := [5]*data.Shift{
		attShift("08:00", "16:00", false), attShift("08:00", "16:00", false),
		attShift("08:00", "16:00", false), attShift("08:00", "16:00", false),
		attShift("08:00", "16:00", false),
	}
	m.Days = [7]*data.Shift{work[0], work[1], work[2], work[3], work[4], nil, nil}

	v := newAttendanceView(&data.ScheduleWeek{
		Week:     attWeek("2026-10-05"),
		Members:  []*data.ScheduleMember{m},
		Absences: []*data.AbsenceRef{attAbsence(1, data.AbsenceVacation, "2026-10-05", "2026-10-07")},
	})
	if got := v.Rows[0].Hours; got != "16 h" {
		t.Errorf("three of five days absent should leave 16 h, got %q", got)
	}
	if got := v.Rows[0].AwayAll; got {
		t.Error("a member with worked days is not away all week")
	}
}

// October must not render as "Oct" or "October" inside a Portuguese document.
func TestAttendanceMonthLabelIsPortuguese(t *testing.T) {
	for _, tc := range []struct{ start, want string }{
		{"2026-01-05", "Janeiro 2026"},
		{"2026-03-02", "Março 2026"},
		{"2026-10-05", "Outubro 2026"},
		{"2026-12-07", "Dezembro 2026"},
	} {
		v := newAttendanceView(&data.ScheduleWeek{Week: attWeek(tc.start)})
		if v.MonthLabel != tc.want {
			t.Errorf("%s: got month %q, want %q", tc.start, v.MonthLabel, tc.want)
		}
	}
}

// The rota keeps a deactivated account as a row on purpose, so a later reactivation
// cannot silently resurrect a row somebody thought they had tidied away. The sheet
// prints every row it is given: the workbook has no concept of a deactivated account,
// and inventing one here would be a difference between the two documents.
func TestAttendanceInactiveMemberIsStillListed(t *testing.T) {
	h := renderOK(t, &data.ScheduleWeek{
		Week: attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{
			{UserID: 1, Name: "Ana", Active: false},
			{UserID: 2, Name: "Bo", Active: true},
		},
	})
	if !strings.Contains(h, ">Ana<") {
		t.Error("a deactivated member must still be printed")
	}
	if !strings.Contains(h, ">Bo<") {
		t.Error("an active member must still be printed")
	}
}

// These need a Chromium-based browser: the pagination bugs they guard only exist in
// the print pipeline. They skip when none is installed, as requireBrowser does.
//
// SIXTEEN members is the measured ceiling for one A4 portrait page, established by
// rendering 6/8/10/12/14/16/18/20/24/30 and counting pages. An earlier estimate of
// twenty came from a row-height budget that ignored the name cell carrying two
// lines, and asserted twenty it came out as two. Pin the measured number; if a
// future density change moves it, this test is where that shows up.
func TestAttendanceSheetIsOnePageAtSixteenMembers(t *testing.T) {
	b := requireBrowser(t)

	html := renderTeamHTML(t, 16)
	pdf, err := b.Render(context.Background(), html)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if n := pageCount(pdf); n != 1 {
		t.Errorf("a 16-member sheet should be one page, got %d", n)
	}
}

// Beyond the ceiling the sheet paginates, which is correct: the <thead> repeats
// from base.css and rows never split, so a second page is a continuation and not a
// second document. This asserts it paginates rather than truncating or exploding.
func TestAttendanceLargeSheetPaginatesCleanly(t *testing.T) {
	b := requireBrowser(t)

	html := renderTeamHTML(t, 30)
	pdf, err := b.Render(context.Background(), html)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if n := pageCount(pdf); n < 2 {
		t.Errorf("a 30-member sheet should need more than one page, got %d", n)
	}
	if n := pageCount(pdf); n > 4 {
		t.Errorf("a 30-member sheet should not balloon to %d pages", n)
	}
}

// renderTeamHTML builds a sheet for n members on a realistic five-day pattern.
func renderTeamHTML(t *testing.T, n int) string {
	t.Helper()
	members := make([]*data.ScheduleMember, 0, n)
	for i := 0; i < n; i++ {
		m := &data.ScheduleMember{
			UserID: int64(i + 1),
			// A deliberately long name: it wraps to two lines, which is the case
			// that sets the row height and therefore the page count.
			Name:   "Funcionário Exemplo Nome Completo",
			Role:   "technician",
			Active: true,
		}
		m.Days = [7]*data.Shift{
			attShift("08:00", "16:00", false), attShift("08:00", "16:00", false),
			attShift("08:00", "16:00", false), attShift("08:00", "16:00", false),
			attShift("08:00", "16:00", false), nil, nil,
		}
		members = append(members, m)
	}
	return renderOK(t, &data.ScheduleWeek{Week: attWeek("2026-10-05"), Members: members})
}

// A short sheet must not gain a blank trailing page. This regressed before, when the
// footer band sat below the content box, so the geometry is asserted rather than
// trusted.
func TestAttendanceSheetDoesNotGainABlankPage(t *testing.T) {
	b := requireBrowser(t)

	m := &data.ScheduleMember{UserID: 1, Name: "Ana", Role: "technician", Active: true}
	m.Days = [7]*data.Shift{attShift("08:00", "16:00", false), nil, nil, nil, nil, nil, nil}

	html, err := RenderAttendanceHTML(&data.ScheduleWeek{
		Week:    attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{m},
	})
	if err != nil {
		t.Fatalf("RenderAttendanceHTML: %v", err)
	}
	pdf, err := b.Render(context.Background(), html)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if n := pageCount(pdf); n != 1 {
		t.Errorf("a single-member sheet should be one page, got %d", n)
	}
}

// The PDF must have the WORKBOOK layout, and the load-bearing detail is that a
// worked day is TWO cells and a word day is ONE merged cell.
//
// The previous version of this document deliberately did the opposite -- one cell per
// day reading "22:00-06:00" -- on the grounds that fourteen columns were too narrow
// in A4 portrait. That reasoning still holds, which is why the sheet is now
// LANDSCAPE, exactly as the workbook own pageSetup asks. The layout itself was never
// the application decision to make: this PDF is the spreadsheet, printed.
func TestAttendanceDayLayoutMatchesTheWorkbook(t *testing.T) {
	m := &data.ScheduleMember{UserID: 1, Name: "Ana", Role: "technician", Active: true}
	m.Days = [7]*data.Shift{attNight("22:00", "06:00"), nil, nil, nil, nil, nil, nil}

	h := renderOK(t, &data.ScheduleWeek{
		Week:    attWeek("2026-10-05"),
		Members: []*data.ScheduleMember{m},
	})

	// A worked day: two separate time cells, Entrada then Saida.
	if !strings.Contains(h, ">22:00<") || !strings.Contains(h, ">06:00<") {
		t.Error("the night should print its two times in two cells")
	}
	// A word day: ONE cell spanning both, which is the merge the workbook has.
	if !strings.Contains(h, `colspan="2">Folga</td>`) {
		t.Error("a day with no shift should be one cell spanning the Entrada/Saida pair")
	}
	// The headings, which are the whole reason the pair exists.
	for _, want := range []string{">Entrada<", ">Saida<", ">Nome<", ">Observação<"} {
		if !strings.Contains(h, want) {
			t.Errorf("the sheet header is missing %s", want)
		}
	}
	if !strings.Contains(h, "size: A4 landscape") {
		t.Error("the sheet should print landscape, matching the workbook pageSetup")
	}
	// Data Entrega is the Monday after the week, and it is on the page.
	if !strings.Contains(h, "12-10-26") {
		t.Error("Data Entrega should be the Monday after the printed week (2026-10-12)")
	}
	// The band chrome the other two reports wear, and the invented key beside it,
	// are gone: the workbook has none of them.
	// Also asserted on markup: base.css is inlined into the document and defines
	// .band-top and .band-bottom whatever this template does with them, so searching
	// for the class NAMES can only ever pass.
	if strings.Contains(h, `<div class="band-top">`) || strings.Contains(h, `<div class="band-bottom">`) {
		t.Error("the sheet must not wear the report header/footer bands; the workbook has none")
	}
}
