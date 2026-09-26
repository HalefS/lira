// Package report renders the application's printable reports.
//
// The daily report is laid out as HTML + CSS and printed by a headless
// Chromium (see chrome.go), which means the printed output reuses the same
// colour palette and typography as the application itself rather than a
// hand-maintained second copy of it. Everything user-supplied is interpolated
// through html/template, so issue text is escaped rather than trusted.
package report

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/data"
)

//go:embed templates/daily.html
var templateFS embed.FS

var dailyTemplate = template.Must(template.ParseFS(templateFS, "templates/daily.html"))

// The application's light-theme palette (the CSS custom properties in the UI's
// :root block). Declared here so the printed report is generated from the same
// values the screen uses; when one of these changes, it changes in both.
const (
	colBlue    = "#1769AA"
	colTeal    = "#1D9E75"
	colAmber   = "#D88A18"
	colGreen   = "#2E8B57"
	colRed     = "#D94B4B"
	colGray    = "#7A8490"
	colGray600 = "#4B5563"
	colGray900 = "#17202A"
)

// avatarPalette mirrors the five .av-N avatar colours the UI assigns by
// avatar_idx, so a technician's initials are the same colour on paper as on
// screen.
var avatarPalette = [5][2]string{
	{"#E8F2FC", "#1769AA"},
	{"#E1F5EE", "#1D9E75"},
	{"#EEF2FF", "#3730A3"},
	{"#FFF4DF", "#D88A18"},
	{"#FDF2F8", "#9D174D"},
}

// fallbackTypeColors is used when an issue's type is no longer in the
// catalog, so its stored color can't be looked up. Keyed by the names the UI
// ships with.
var fallbackTypeColors = map[string]string{
	"Door":     "#1769AA",
	"Internet": "#1D9E75",
	"Hardware": "#D88A18",
	"TV":       "#9D174D",
	"AC":       "#3730A3",
	"Phone":    "#854F0B",
	"Other":    "#4B5563",
}

// unknownTypeColor is the last resort, for a type that is neither in the
// catalog nor one of the built-in names.
const unknownTypeColor = "#4B5563"

// maxTechBars bounds the technician column so a large team can't stretch the
// summary grid over a whole page. The remainder is reported as a count rather
// than silently dropped.
const maxTechBars = 15

// statCard is one figure in the summary grid.
type statCard struct {
	Label string
	Value string
	Sub   string
	Color string
}

// typeBar is one row of the "issues by type" chart.
type typeBar struct {
	Name  string
	Color string
	Count int
	Pct   float64
}

// techBar is one row of the "by technician" chart.
type techBar struct {
	Name       string
	Initials   string
	AvatarBG   string
	AvatarFG   string
	Count      int
	Pct        float64
	Overflow   int
	IsOverflow bool
}

// issueRow is one line of an issue table, with everything the template needs
// already resolved so the template stays free of logic.
type issueRow struct {
	Location      string
	Type          string
	TypeColor     string
	Problem       string
	Resolution    string
	Time          string
	Status        string
	StatusClass   string
	Tech          string
	Initials      string
	AvatarBG      string
	AvatarFG      string
	FalsePositive bool
}

// dailyView is the whole template's data.
type dailyView struct {
	Date        string
	DateLong    string
	GeneratedAt string
	Summary     []statCard
	Types       []typeBar
	Techs       []techBar
	AptIssues   []issueRow
	DeptIssues  []issueRow
	TotalIssues int
	Empty       bool
}

// RenderDailyHTML renders the daily report as a standalone HTML document.
func RenderDailyHTML(r *data.DailyReport) (string, error) {
	if r == nil {
		return "", fmt.Errorf("report: nil daily report")
	}

	var buf bytes.Buffer
	if err := dailyTemplate.Execute(&buf, newDailyView(r)); err != nil {
		return "", fmt.Errorf("report: rendering daily template: %w", err)
	}
	return buf.String(), nil
}

func newDailyView(r *data.DailyReport) dailyView {
	s := r.Summary

	fpCount := s.FalsePositives
	realCount := s.TotalIssues - fpCount
	fpRate := 0
	if s.TotalIssues > 0 {
		fpRate = int(math.Round(float64(fpCount) / float64(s.TotalIssues) * 100))
	}

	dateLong := r.Date
	if t, err := time.Parse("2006-01-02", r.Date); err == nil {
		dateLong = t.Format("Monday, 2 January 2006")
	}

	return dailyView{
		Date:        r.Date,
		DateLong:    dateLong,
		GeneratedAt: r.GeneratedAt.Format("2 Jan 2006, 15:04"),
		TotalIssues: s.TotalIssues,
		Empty:       s.TotalIssues == 0,
		Summary: []statCard{
			{"Total issues", itoa(s.TotalIssues), "Apartments + departments", colBlue},
			{"Resolved", itoa(s.Resolved), pct(s.ResolutionRate) + " resolution rate", colGreen},
			{"Pending", itoa(s.Pending), "Still open", colAmber},
			{"Avg. time", trimFloat(s.AvgMinutes) + " min", "Per issue", colTeal},
			{"Total time", itoa(s.TotalMinutes) + " min", "All issues combined", colBlue},
			{"Fastest fix", itoa(s.FastestMinutes) + " min", "Quickest resolution", colGreen},
			{"Slowest fix", itoa(s.SlowestMinutes) + " min", "Longest resolution", colAmber},
			{"Apt. issues", itoa(s.AptIssues), "Apartments", colBlue},
			{"Dept. issues", itoa(s.DeptIssues), "Departments", colTeal},
			{"False positives", itoa(fpCount), "Nothing was wrong", colAmber},
			{"False positive rate", itoa(fpRate) + "%", "Of all issues logged", colAmber},
			{"Real issues", itoa(realCount), "Something was wrong", colGreen},
		},
		Types:      typeBars(r),
		Techs:      techBars(r),
		AptIssues:  issueRows(r.AptIssues, r.TypeColors),
		DeptIssues: issueRows(r.DeptIssues, r.TypeColors),
	}
}

// typeBars turns the by-type counts into chart rows, tallest first. The bar
// widths are relative to the largest count, so the chart always fills its
// column instead of leaving a gap when the numbers are small.
func typeBars(r *data.DailyReport) []typeBar {
	if len(r.ByType) == 0 {
		return nil
	}

	names := make([]string, 0, len(r.ByType))
	max := 0
	for name, count := range r.ByType {
		names = append(names, name)
		if count > max {
			max = count
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if r.ByType[names[i]] != r.ByType[names[j]] {
			return r.ByType[names[i]] > r.ByType[names[j]]
		}
		return names[i] < names[j]
	})

	bars := make([]typeBar, 0, len(names))
	for _, name := range names {
		bars = append(bars, typeBar{
			Name:  name,
			Color: typeColor(name, r.TypeColors),
			Count: r.ByType[name],
			Pct:   barWidth(r.ByType[name], max),
		})
	}
	return bars
}

// techBars does the same for the per-technician counts, keeping each
// technician's avatar color. Technicians who logged nothing are dropped: they
// say nothing on a printed report, and there can be many of them.
//
// Sorted here rather than trusted to arrive in order — the query already orders
// by count, but a chart that silently depends on its caller's sort order is
// one refactor away from being wrong.
func techBars(r *data.DailyReport) []techBar {
	max := 0
	for _, t := range r.ByTechnician {
		if t.Count > max {
			max = t.Count
		}
	}
	if max == 0 {
		return nil
	}

	sorted := make([]data.TechStat, 0, len(r.ByTechnician))
	for _, t := range r.ByTechnician {
		if t.Count > 0 {
			sorted = append(sorted, t)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Count != sorted[j].Count {
			return sorted[i].Count > sorted[j].Count
		}
		return sorted[i].Name < sorted[j].Name
	})

	bars := make([]techBar, 0, maxTechBars+1)
	for i, t := range sorted {
		if i == maxTechBars {
			bars = append(bars, techBar{Overflow: len(sorted) - maxTechBars, IsOverflow: true})
			break
		}
		bg, fg := avatarColors(t.AvatarIdx)
		bars = append(bars, techBar{
			Name:     t.Name,
			Initials: initials(t.Name),
			AvatarBG: bg,
			AvatarFG: fg,
			Count:    t.Count,
			Pct:      barWidth(t.Count, max),
		})
	}
	return bars
}

// barWidth expresses count as a percentage of the largest count, never zero —
// a logged-but-tiny count should still show a visible sliver.
func barWidth(count, max int) float64 {
	if max <= 0 {
		return 0
	}
	w := float64(count) / float64(max) * 100
	if w < 2 {
		return 2
	}
	return w
}

func issueRows(issues []*data.Issue, typeColors map[string]string) []issueRow {
	rows := make([]issueRow, 0, len(issues))
	for _, i := range issues {
		if i == nil {
			continue
		}
		bg, fg := avatarColors(i.LoggedByIdx)
		rows = append(rows, issueRow{
			Location:      i.Location,
			Type:          i.Type,
			TypeColor:     typeColor(i.Type, typeColors),
			Problem:       i.Problem,
			Resolution:    orDash(i.Resolution),
			Time:          itoa(i.TimeMinutes) + " min",
			Status:        i.Status,
			StatusClass:   statusClass(i.Status),
			Tech:          i.LoggedByName,
			Initials:      initials(i.LoggedByName),
			AvatarBG:      bg,
			AvatarFG:      fg,
			FalsePositive: i.FalsePositive,
		})
	}
	return rows
}

// typeColor resolves the color a type is drawn in: the manager's stored choice
// when it is a usable hex value, otherwise the built-in name match.
//
// The hex check is not defensive padding. Rows created before colors became
// editable (migration 000009) hold CSS class names such as "badge-door", and
// a manager only replaces those by opening the color picker, so an untouched
// legacy row must fall back rather than emit an invalid color.
func typeColor(name string, colors map[string]string) string {
	if c, ok := colors[name]; ok && isHexColor(c) {
		return c
	}
	if c, ok := fallbackTypeColors[name]; ok {
		return c
	}
	return unknownTypeColor
}

func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func avatarColors(idx int) (bg, fg string) {
	if idx < 0 {
		idx = 0
	}
	p := avatarPalette[idx%len(avatarPalette)]
	return p[0], p[1]
}

func statusClass(status string) string {
	if status == "Ok" {
		return "ok"
	}
	return "pending"
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// initials mirrors the UI's Avatar: the first letter of each word, at most
// two, upper-cased.
func initials(name string) string {
	var letters []rune
	for _, word := range strings.Fields(name) {
		if len(letters) == 2 {
			break
		}
		letters = append(letters, []rune(word)[0])
	}
	return strings.ToUpper(string(letters))
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func pct(f float64) string { return fmt.Sprintf("%.0f%%", f) }

// trimFloat drops a trailing ".0" so an average of exactly 12 minutes reads
// "12" rather than "12.0".
func trimFloat(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(s, ".0")
}
