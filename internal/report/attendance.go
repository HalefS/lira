package report

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/data"
)

// The weekly attendance sheet -- "Folha de Ponto/Turnos" -- rendered server-side and
// printed by headless Chromium, exactly like the daily and consumables reports.
//
// The document is PORTUGUESE, unconditionally. It is not a translation of an English
// report: its title, its column heads and its status words are the vocabulary of a
// Portuguese HR form, and the user's own Excel twin of this artefact is written in
// Portuguese. The application chrome around the button that downloads it stays
// bilingual; the artefact does not. A ?lang= variant would mean a translated label
// table here plus contextGetUser(r).Language as the source for an authenticated
// caller -- a door deliberately left unopened rather than a half-built feature.
//
// "Feriado" is deliberately ABSENT from the status words below. The Excel template
// has it, but the application has no public-holiday table, no holiday column and no
// holiday endpoint: internal/data/absence.go models exactly four kinds and none of
// them is a public holiday. Printing a word nothing can produce is worse than not
// printing it -- it is a promise the data cannot keep.

// attendanceMonths and attendanceWeekdays are spelled out rather than taken from
// time.Format, which would render October as "Oct" and "January" as "January" inside
// a Portuguese document. Consumables does exactly that today ("2 Jan 2006"), which
// is fine for an internal English diagnostic and wrong here.
//
// "Sábado" and "Saída" carry their accents even though the Excel template omits them.
// Those are structural headings, not data vocabulary, and a new document should not
// inherit a spelling mistake. The status words DO match the template exactly -- see
// attendanceStatus below.
var attendanceMonths = [...]string{
	"Janeiro", "Fevereiro", "Março", "Abril", "Maio", "Junho",
	"Julho", "Agosto", "Setembro", "Outubro", "Novembro", "Dezembro",
}

var attendanceWeekdays = [...]string{
	"Segunda", "Terça", "Quarta", "Quinta", "Sexta", "Sábado", "Domingo",
}

// attendanceStatus maps an absence kind to the word the sheet prints in that cell.
//
// Three of the four come from the user's own Excel template and are reproduced
// character-for-character, accents and all, so the two artefacts cannot disagree
// about the same week: "Folga" for a day off, "Feria" for vacation (the template
// uses it ten times across its two later weeks) and "Falta" for sick leave. "Feria"
// rather than the correct "Férias" is a deliberate match, not a typo -- normalising
// it here would make the PDF and the spreadsheet read as two different documents.
//
// "Formação" and "Indisponível" are additions: the schema has those two kinds and
// the template has no word for either, so they needed inventing. Both are plain
// Portuguese and neither is confusable with the other three.
var attendanceStatus = map[string]string{
	data.AbsenceVacation:    "Feria",
	data.AbsenceSick:        "Falta",
	data.AbsenceTraining:    "Formação",
	data.AbsenceUnavailable: "Indisponível",
}

// dayOffWord is what a nil shift with no absence prints. Never an empty cell: in a
// bordered grid a blank reads as still-loading, and a sheet that looks like it is
// still loading looks broken. This document gets signed.
const dayOffWord = "Folga"

// attendanceDayColumn is one weekday's column header. Date is "DD-MM" because the
// dates are printed in European order, matching the dd-mm-yy number format the Excel
// twin now carries.
type attendanceDayColumn struct {
	Name    string
	Date    string
	Weekday int
	IsToday bool
}

// attendanceCell is one member-day. Exactly one of Times or Status is set: a day is
// either worked or it carries a word, and there is no third state.
//
// Status wins over Times, which is the precedence the rota page already applies --
// an absence beats the recurring shift for that day. On a printed sheet only one
// thing can be rendered per day, so the absence is printed and the shift is not.
// That is a rendering decision, not a data claim: Days[i] still holds the shift in
// the database, so a manager can see what the rota has them on that weekday and move
// it when the absence ends. Precedence is not deletion.
type attendanceCell struct {
	Times     string // "22:00-06:00", or "" when Status is set
	Status    string // "Folga" / "Feria" / "Falta" / "Formação" / "Indisponível"
	Overnight bool
	Tint      string
}

// Label is the cell as one string, which is how a rota cell reads. It follows
// Shift.Label in leaving the overnight case undecorated: on screen and on paper
// alike, "22:00-06:00" is what every rota in the world writes, and Overnight is
// there for code that needs to know rather than for the label.
func (c attendanceCell) Label() string {
	if c.Status != "" {
		return c.Status
	}
	return c.Times
}

// Start and End are the two clock times of a worked day, split back out of the one
// string, for the two renderers that need them apart: the spreadsheet prints one
// under the Entrada heading and one under Saida, and so does the PDF, because the
// PDF is a conversion of that workbook rather than its own design.
//
// Splitting on the hyphen is safe here because these are the server own times,
// written "HH:MM" with exactly one hyphen between them. Both return "" for a day
// carrying a status word, so a caller can use Start alone to ask "is this worked?".
func (c attendanceCell) Start() string {
	if c.Status != "" {
		return ""
	}
	if i := strings.IndexByte(c.Times, '-'); i >= 0 {
		return c.Times[:i]
	}
	return c.Times
}

func (c attendanceCell) End() string {
	if c.Status != "" {
		return ""
	}
	if i := strings.IndexByte(c.Times, '-'); i >= 0 {
		return c.Times[i+1:]
	}
	return ""
}

// attendanceRow is one member.
type attendanceRow struct {
	Name     string
	Initials string
	Inactive bool

	// AwayAll and NotOnRota are the distinction the rota page refuses to collapse:
	// "away all week" and "not on this rota" are different facts that produce an
	// identical grid of seven word-cells and no times. Only the name cell can carry
	// the difference, so it must carry it.
	AwayAll   bool
	NotOnRota bool
	AwayWord  string
	Hours     string // "40 h", the hours actually worked, absences excluded
	Cells     []attendanceCell
}

// attendanceLegendEntry is one line of the key printed beside the signature block.
type attendanceLegendEntry struct {
	Label  string
	Times  string
	Count  int
	IsWord bool
	Hue    string
}

// attendanceView is the whole document.
type attendanceView struct {
	MonthLabel string
	WeekLabel  string
	// DeliveryLabel is Data Entrega: the Monday AFTER the week being printed, so a
	// sheet covering Mon 5th to Sun 11th is handed in on Mon 12th. That is the
	// template own rule -- its four sample weeks each carry a delivery date exactly
	// seven days after their Monday -- so the export follows the workbook rather
	// than inventing a second convention beside it.
	//
	// It lives here, formatted once, because the two renderers write it two
	// different ways: the spreadsheet as a date serial and the PDF as text. One
	// field, two presentations, no chance of them disagreeing.
	DeliveryLabel string
	FromDate      string
	ToDate        string
	RangeLabel    string
	GeneratedAt   string

	Days    []attendanceDayColumn
	Rows    []attendanceRow
	Turnos  []attendanceLegendEntry
	Motivos []attendanceLegendEntry

	// FootNote explains the "+1" marker, and is empty unless some shift in the week
	// is overnight. A week with no nights does not carry a line about nothing.
	FootNote     string
	HasOvernight bool
	Empty        bool
}

// RenderAttendanceHTML renders the weekly attendance sheet as a standalone HTML
// document. A nil week, or one with no Week header, is an error rather than a blank
// sheet: a signed document that silently renders nothing is worse than a refusal.
func RenderAttendanceHTML(w *data.ScheduleWeek) (string, error) {
	if w == nil {
		return "", fmt.Errorf("report: nil schedule week")
	}
	if w.Week == nil {
		return "", fmt.Errorf("report: schedule week has no week header")
	}

	var buf bytes.Buffer
	if err := attendanceTemplate.Execute(&buf, newAttendanceView(w)); err != nil {
		return "", fmt.Errorf("report: rendering attendance template: %w", err)
	}
	return buf.String(), nil
}

// absenceIndex flattens absences to (user, date) -> kind.
//
// First match wins, and the input is already ordered by user_id, starts_on, id --
// which is what the rota page relies on. The schema permits two absences to cover
// the same date, so "first" is a real decision rather than a formality: printing
// both would need a combined word nobody asked for, and summing hours across them
// would double-count a day.
func absenceIndex(list []*data.AbsenceRef) map[absenceKey]string {
	out := make(map[absenceKey]string, len(list)*2)
	for _, a := range list {
		if a == nil {
			continue
		}
		for d := a.StartsOn.Time(); !d.After(a.EndsOn.Time()); d = d.AddDate(0, 0, 1) {
			k := absenceKey{a.UserID, d.Format(time.DateOnly)}
			if _, seen := out[k]; !seen {
				out[k] = a.Kind
			}
		}
	}
	return out
}

type absenceKey struct {
	user int64
	date string
}

func newAttendanceView(w *data.ScheduleWeek) attendanceView {
	abs := absenceIndex(w.Absences)

	// ONE anchor for the whole document.
	//
	// Snapped to Monday, so a caller anchoring on any other day still gets a
	// Monday-to-Sunday sheet rather than one that starts on whatever day it was
	// handed. Every date, name and lookup below comes from this value and nothing
	// else.
	mon := mondayOf(w.Week.Start.Time())
	end := mon.AddDate(0, 0, 6)
	delivery := mon.AddDate(0, 0, 7)

	v := attendanceView{
		MonthLabel:    attendanceMonths[int(mon.Month())-1] + " " + mon.Format("2006"),
		WeekLabel:     mon.Format("02-01") + " a " + end.Format("02-01"),
		DeliveryLabel: delivery.Format("02-01-06"),
		FromDate:      mon.Format(time.DateOnly),
		ToDate:        end.Format(time.DateOnly),
		RangeLabel:    mon.Format("02-01-2006") + " a " + end.Format("02-01-2006"),
		GeneratedAt:   time.Now().Format("02/01/2006 15:04"),
		HasOvernight:  false,
	}

	// The seven column heads are COUNTED OFF THE ANCHOR, not read out of the
	// payload. Week.Days is consulted for exactly one thing, IsToday, and it is
	// looked up BY DATE rather than by position, so a payload whose day rows are
	// offset cannot shift a column.
	//
	// The weekday NAME is the index into the Portuguese table and never the payload
	// own label: WeekDay.Label is the server short label and it is ENGLISH -- the
	// live payload carries "Mon", "Tue", "Wed" -- so preferring it printed an English
	// day head on a Portuguese document.
	todayByDate := map[string]bool{}
	for _, d := range w.Week.Days {
		todayByDate[d.Date.Time().Format(time.DateOnly)] = d.IsToday
	}
	v.Days = make([]attendanceDayColumn, 0, 7)
	for i := 0; i < 7; i++ {
		d := mon.AddDate(0, 0, i)
		v.Days = append(v.Days, attendanceDayColumn{
			Name:    attendanceWeekdays[i],
			Date:    d.Format("02-01"),
			Weekday: i + 1,
			IsToday: todayByDate[d.Format(time.DateOnly)],
		})
	}

	shiftPeople := map[string]int{}
	shiftTimes := map[string]string{}
	shiftOrder := []string{}
	wordPeople := map[string]int{}
	wordOrder := []string{}

	for _, m := range w.Members {
		if m == nil {
			continue
		}
		row := attendanceRow{
			Name:     m.Name,
			Initials: initials(m.Name),
			Inactive: !m.Active,
			Hours:    "0 h",
		}

		worked := 0
		awayDays := 0
		for i := 0; i < 7; i++ {
			cell := attendanceCell{}
			// The same anchor as the columns above. Reading this from Week.Days was
			// the third place the week's dates came from, and the second that could
			// disagree with the label printed above the table.
			date := mon.AddDate(0, 0, i).Format(time.DateOnly)

			kind, isAbsent := abs[absenceKey{m.UserID, date}]
			switch {
			case isAbsent:
				cell.Status = attendanceStatus[kind]
				if cell.Status == "" {
					cell.Status = "Indisponível"
				}
				awayDays++
				if _, seen := wordPeople[cell.Status]; !seen {
					wordOrder = append(wordOrder, cell.Status)
				}
				wordPeople[cell.Status]++
			case i < len(m.Days) && m.Days[i] != nil:
				s := m.Days[i]
				cell.Times = s.Label()
				cell.Overnight = s.Overnight
				cell.Tint = s.Color
				worked += s.DurationMinutes()
				v.HasOvernight = v.HasOvernight || s.Overnight
				if _, seen := shiftPeople[s.Name]; !seen {
					shiftOrder = append(shiftOrder, s.Name)
					// Take the legend times from the shift itself, so the key cannot
					// disagree with the cells about what "Night" means.
					shiftTimes[s.Name] = s.Label()
				}
				shiftPeople[s.Name]++
			default:
				cell.Status = dayOffWord
			}
			row.Cells = append(row.Cells, cell)
		}

		// Away all week is the screen's rule exactly: no assignment at all AND an
		// absence on every one of the seven days. Anything less is not "away".
		row.AwayAll = worked == 0 && awayDays == 7
		row.NotOnRota = worked == 0 && !row.AwayAll
		if row.AwayAll {
			for _, c := range row.Cells {
				if c.Status != "" {
					row.AwayWord = c.Status
					break
				}
			}
		}
		if h := worked / 60; worked > 0 {
			row.Hours = fmt.Sprintf("%d h", h)
		}
		v.Rows = append(v.Rows, row)
	}

	for _, name := range shiftOrder {
		v.Turnos = append(v.Turnos, attendanceLegendEntry{
			Label: name,
			Times: shiftTimes[name],
			Count: shiftPeople[name],
		})
	}
	for _, word := range wordOrder {
		v.Motivos = append(v.Motivos, attendanceLegendEntry{
			Label:  word,
			Count:  wordPeople[word],
			IsWord: true,
			Hue:    absenceHue(word),
		})
	}

	if v.HasOvernight {
		v.FootNote = "+1 = o turno termina no dia seguinte."
	}
	v.Empty = len(v.Rows) == 0

	return v
}

// absenceHue gives an absence word the same ink the application uses for that kind,
// so a printed sheet and the screen agree on what colour "Falta" is.
func absenceHue(word string) string {
	switch word {
	case "Feria":
		return "var(--violet)"
	case "Falta":
		return "var(--teal)"
	case "Formação":
		return "var(--green)"
	default:
		return "var(--gray-600)"
	}
}

// mondayOf snaps a date back to the Monday of its week. It lives here rather than
// beside either renderer because Data Entrega is the Monday AFTER the printed week
// and both renderers need that same anchor: agreeing on where the week starts is the
// one thing that must not be decided twice.
func mondayOf(t time.Time) time.Time {
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -int(d.Weekday())+1)
}
