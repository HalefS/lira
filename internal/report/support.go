package report

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/HalefS/lira/internal/data"
)

// The third-party support report. It is the mirror of the issue reports: those
// deliberately leave handed-over issues out, and this is where those handovers
// are accounted for instead. Same rendering path as the other reports -- an HTML
// document printed by a headless Chromium -- so it inherits the application's
// palette, typography and page geometry rather than keeping a second copy.

// supportStatCard is one figure in the headline row.
type supportStatCard struct {
	Label string
	Value string
	Sub   string
	Color string
}

// supportCompanyRow is one line of the by-company table, including the combined
// "both companies" line at the end.
type supportCompanyRow struct {
	Name    string
	Color   string
	Overall bool
	Total   string
	Solved  string
	Pending string
	Timed   string
	Avg     string
	Median  string
	Fastest string
	Slowest string
}

// supportRow is one handover in the detail table. Everything is preformatted so
// the template only has to place text, never decide how a number reads.
type supportRow struct {
	IssueID      string
	Location     string
	Problem      string
	Company      string
	CompanyColor string
	Reference    string
	ReportedDate string
	ReportedTime string
	SolvedDate   string
	SolvedTime   string
	Duration     string
	Status       string
	StatusClass  string
	Note         string
	Pending      bool
}

type supportView struct {
	RangeLabel  string
	GeneratedAt string
	Summary     []supportStatCard
	Companies   []supportCompanyRow
	Rows        []supportRow
	Total       int
	Empty       bool
}

// RenderSupportHTML renders the third-party support report as a standalone HTML
// document.
func RenderSupportHTML(r *data.SupportReport) (string, error) {
	if r == nil {
		return "", fmt.Errorf("report: nil support report")
	}
	if r.Stats == nil || r.Stats.Overall == nil {
		return "", fmt.Errorf("report: support report has no statistics")
	}

	var buf bytes.Buffer
	if err := supportTemplate.Execute(&buf, newSupportView(r)); err != nil {
		return "", fmt.Errorf("report: rendering support template: %w", err)
	}
	return buf.String(), nil
}

func newSupportView(r *data.SupportReport) supportView {
	o := r.Stats.Overall

	v := supportView{
		RangeLabel:  supportRangeLabel(r.From, r.To),
		GeneratedAt: r.GeneratedAt.Format("2 Jan 2006, 15:04"),
		Total:       len(r.Rows),
		Empty:       len(r.Rows) == 0,
	}

	v.Summary = []supportStatCard{
		{"Handovers", itoa(o.Total), "all companies", colBlue},
		{"Resolved", itoa(o.Resolved), "answer received", colGreen},
		{"Still waiting", itoa(o.Pending), "no answer yet", colAmber},
		{"Avg. turnaround", supportDuration(o.AvgMinutes), "per resolved handover", colTeal},
		{"Median turnaround", supportDuration(o.MedianMinutes), "half are quicker", colTeal},
		{"Fastest", supportDuration(float64(o.FastestMinutes)), "quickest answer", colGreen},
		{"Slowest", supportDuration(float64(o.SlowestMinutes)), "longest answer", colAmber},
		{"Timed", itoa(o.Timed), "with a duration", colBlue},
	}

	for _, s := range r.Stats.ByCompany {
		v.Companies = append(v.Companies, supportCompanyRow{
			Name:    supportCompanyName(s.Name, s.Company),
			Color:   supportCompanyColor(s.Color),
			Total:   itoa(s.Total),
			Solved:  itoa(s.Resolved),
			Pending: itoa(s.Pending),
			Timed:   itoa(s.Timed),
			Avg:     supportDuration(s.AvgMinutes),
			Median:  supportDuration(s.MedianMinutes),
			Fastest: supportDuration(float64(s.FastestMinutes)),
			Slowest: supportDuration(float64(s.SlowestMinutes)),
		})
	}
	v.Companies = append(v.Companies, supportCompanyRow{
		Name:    "All companies",
		Color:   colBlue,
		Overall: true,
		Total:   itoa(o.Total),
		Solved:  itoa(o.Resolved),
		Pending: itoa(o.Pending),
		Timed:   itoa(o.Timed),
		Avg:     supportDuration(o.AvgMinutes),
		Median:  supportDuration(o.MedianMinutes),
		Fastest: supportDuration(float64(o.FastestMinutes)),
		Slowest: supportDuration(float64(o.SlowestMinutes)),
	})

	for _, row := range r.Rows {
		name := supportCompanyName(row.CompanyName, row.Company)
		color := supportCompanyColor(row.CompanyColor)
		status, statusClass := supportStatus(row.Status)
		reportedDate, reportedTime := supportMoment(row.StartedAt)
		solvedDate, solvedTime := supportMoment(row.ResolvedAt)
		duration := "—"
		if row.DurationMinutes != nil {
			duration = supportDuration(float64(*row.DurationMinutes))
		}
		v.Rows = append(v.Rows, supportRow{
			IssueID:      strconv.FormatInt(row.IssueID, 10),
			Location:     row.IssueLocation,
			Problem:      row.IssueProblem,
			Company:      name,
			CompanyColor: color,
			Reference:    orDash(row.Reference),
			ReportedDate: reportedDate,
			ReportedTime: reportedTime,
			SolvedDate:   solvedDate,
			SolvedTime:   solvedTime,
			Duration:     duration,
			Status:       status,
			StatusClass:  statusClass,
			Note:         row.Notes,
			Pending:      row.Status == data.SupportStatusPending,
		})
	}

	return v
}

// supportRangeLabel names the window the report covers. The page can open on
// "All time", so an unbounded end is described rather than printed as a blank.
func supportRangeLabel(from, to string) string {
	if from == "" && to == "" {
		return "All time"
	}
	f, ferr := time.Parse("2006-01-02", from)
	t, terr := time.Parse("2006-01-02", to)
	switch {
	case ferr == nil && terr == nil:
		return dateRangeLabel(f, t)
	case terr == nil:
		return "Up to " + t.Format("2 Jan 2006")
	case ferr == nil:
		return "From " + f.Format("2 Jan 2006")
	default:
		return "All time"
	}
}

// supportDuration renders minutes the way the support page does: days, hours
// and minutes, singular or plural, so the printed figure reads like the one on
// screen instead of an unexplained count of minutes.
func supportDuration(minutes float64) string {
	total := int(math.Round(minutes))
	if total <= 0 {
		return "0m"
	}
	days := total / 1440
	hours := (total % 1440) / 60
	mins := total % 60
	// Compact units rather than spelled-out plurals: "1h 30m" reads as a
	// duration at a glance, while "30ms" reads as milliseconds.
	unit := func(n int, suffix string) string { return strconv.Itoa(n) + suffix }
	switch {
	case days > 0 && hours > 0:
		return unit(days, "d") + " " + unit(hours, "h")
	case days > 0:
		return unit(days, "d")
	case hours > 0 && mins > 0:
		return unit(hours, "h") + " " + unit(mins, "m")
	case hours > 0:
		return unit(hours, "h")
	default:
		return unit(mins, "m")
	}
}

// supportCompanyName is the catalog's display name, with the two built-in slugs
// as a fallback for a report built from older data.
func supportCompanyName(name, slug string) string {
	if name != "" {
		return name
	}
	switch slug {
	case data.SupportCompanyTelnet:
		return "Telnet"
	case data.SupportCompanyTelefonica:
		return "Telefónica"
	default:
		return slug
	}
}

// supportCompanyColor maps a catalog colour token onto the report's palette.
func supportCompanyColor(name string) string {
	switch name {
	case "blue":
		return colBlue
	case "teal":
		return colTeal
	case "amber":
		return colAmber
	case "green":
		return colGreen
	case "red":
		return colRed
	default:
		return colGray
	}
}

func supportStatus(status string) (label, class string) {
	if status == data.SupportStatusSolved {
		return "OK", "badge-ok"
	}
	return "Pending", "badge-pending"
}

func supportMoment(t *time.Time) (date, clock string) {
	if t == nil || t.IsZero() {
		return "—", ""
	}
	return t.Format("2 Jan 2006"), t.Format("15:04")
}
