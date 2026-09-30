package report

import (
	"bytes"
	"fmt"
	"time"

	"github.com/HalefS/lira/internal/data"
)

// The weekly consumables report. Same rendering path as the daily report — an
// HTML document printed by a headless Chromium — so it inherits the app's
// palette, typography and page geometry rather than keeping a second copy.

// consumableStat is one figure in the headline row.
type consumableStat struct {
	Label string
	Value string
	Sub   string
	Color string
}

// consumableItemRow is one line of the "by item" table.
type consumableItemRow struct {
	Item     string
	Icon     string
	Quantity int
	Records  int
	Pct      float64
}

// consumableLocationRow is one line of the "by location" table.
type consumableLocationRow struct {
	Location string
	Mode     string
	Quantity int
	Records  int
	Pct      float64
}

// consumableTechRow is one line of the "by technician" table.
type consumableTechRow struct {
	Name     string
	Initials string
	AvatarBG string
	AvatarFG string
	Quantity int
	Records  int
	Pct      float64
}

// consumableDayRow is one column of the per-day bar chart.
type consumableDayRow struct {
	Label    string
	Weekday  string
	Quantity int
	Height   float64
	Empty    bool
}

// consumableEntryRow is one line of the detail table: what was used, where,
// on which issue, by whom and when.
type consumableEntryRow struct {
	Date        string
	Time        string
	Item        string
	Icon        string
	Quantity    int
	Mode        string
	Location    string
	IssueType   string
	IssueID     int64
	Problem     string
	Tech        string
	Initials    string
	AvatarBG    string
	AvatarFG    string
	UnknownTech bool
}

type consumablesView struct {
	FromDate    string
	ToDate      string
	RangeLabel  string
	GeneratedAt string

	Stats    []consumableStat
	Items    []consumableItemRow
	Location []consumableLocationRow
	Techs    []consumableTechRow
	Days     []consumableDayRow
	Entries  []consumableEntryRow

	// Split of the detail table into the two kinds of place, so the location
	// names a reader cares about are grouped rather than interleaved.
	AptEntries  []consumableEntryRow
	DeptEntries []consumableEntryRow

	Empty bool
}

// RenderConsumablesHTML renders the weekly consumables report as a standalone
// HTML document.
func RenderConsumablesHTML(r *data.ConsumablesWeeklyReport) (string, error) {
	if r == nil {
		return "", fmt.Errorf("report: nil consumables report")
	}

	var buf bytes.Buffer
	if err := consumablesTemplate.Execute(&buf, newConsumablesView(r)); err != nil {
		return "", fmt.Errorf("report: rendering consumables template: %w", err)
	}
	return buf.String(), nil
}

func newConsumablesView(r *data.ConsumablesWeeklyReport) consumablesView {
	max := 0
	for _, t := range r.ByItem {
		if t.Quantity > max {
			max = t.Quantity
		}
	}

	v := consumablesView{
		FromDate:    r.FromDate,
		ToDate:      r.ToDate,
		RangeLabel:  dateRangeLabel(r.From, r.To.AddDate(0, 0, -1)),
		GeneratedAt: r.GeneratedAt.Format("2 Jan 2006, 15:04"),
		Empty:       r.TotalRecords == 0,
	}

	v.Stats = []consumableStat{
		{"Items used", itoa(r.TotalQuantity), "total units", colBlue},
		{"Records", itoa(r.TotalRecords), "logged entries", colTeal},
		{"Distinct items", itoa(r.DistinctItems), "from the catalog", colGreen},
		{"Locations", itoa(r.LocationsCount), "apartments + departments", colAmber},
	}

	for _, t := range r.ByItem {
		v.Items = append(v.Items, consumableItemRow{
			Item:     t.Item,
			Icon:     t.Icon,
			Quantity: t.Quantity,
			Records:  t.Records,
			Pct:      barWidth(t.Quantity, max),
		})
	}

	locMax := maxLocation(r.ByLocation)
	for _, t := range r.ByLocation {
		v.Location = append(v.Location, consumableLocationRow{
			Location: t.Location,
			Mode:     t.Mode,
			Quantity: t.Quantity,
			Records:  t.Records,
			Pct:      barWidth(t.Quantity, locMax),
		})
	}

	techMax := 0
	for _, t := range r.ByTechnician {
		if t.Quantity > techMax {
			techMax = t.Quantity
		}
	}
	for _, t := range r.ByTechnician {
		bg, fg := avatarColors(t.AvatarIdx)
		name := t.Name
		v.Techs = append(v.Techs, consumableTechRow{
			Name:     name,
			Initials: initials(name),
			AvatarBG: bg,
			AvatarFG: fg,
			Quantity: t.Quantity,
			Records:  t.Records,
			Pct:      barWidth(t.Quantity, techMax),
		})
	}

	// One bar per day of the week, so a quiet day is visibly quiet instead of
	// missing. Days with no usage still get a slot.
	byDate := make(map[string]int, len(r.ByDay))
	for _, d := range r.ByDay {
		byDate[d.Date] = d.Quantity
	}
	dayMax := 0
	for _, d := range r.ByDay {
		if d.Quantity > dayMax {
			dayMax = d.Quantity
		}
	}
	for d := r.From; d.Before(r.To); d = d.AddDate(0, 0, 1) {
		q := byDate[d.Format("2006-01-02")]
		row := consumableDayRow{
			Label:    d.Format("2 Jan"),
			Weekday:  d.Format("Mon"),
			Quantity: q,
			Empty:    q == 0,
		}
		if dayMax > 0 {
			// Floor the bar so a single unit is still visible, and cap it so a
			// tall day does not flatten the rest.
			h := float64(q) / float64(dayMax) * 100
			if h < 4 && q > 0 {
				h = 4
			}
			row.Height = h
		}
		v.Days = append(v.Days, row)
	}

	for _, e := range r.Entries {
		bg, fg := avatarColors(e.LoggedByIdx)
		row := consumableEntryRow{
			Date:        e.CreatedAt.Format("2 Jan"),
			Time:        e.CreatedAt.Format("15:04"),
			Item:        e.Item,
			Icon:        e.Icon,
			Quantity:    e.Quantity,
			Mode:        e.Mode,
			Location:    e.Location,
			IssueType:   e.IssueType,
			IssueID:     e.IssueID,
			Problem:     e.IssueProblem,
			Tech:        e.LoggedByName,
			Initials:    initials(e.LoggedByName),
			AvatarBG:    bg,
			AvatarFG:    fg,
			UnknownTech: e.LoggedBy == nil,
		}
		v.Entries = append(v.Entries, row)
		if e.Mode == "apt" {
			v.AptEntries = append(v.AptEntries, row)
		} else {
			v.DeptEntries = append(v.DeptEntries, row)
		}
	}

	return v
}

func maxLocation(rows []*data.ConsumableLocationTotal) int {
	max := 0
	for _, t := range rows {
		if t.Quantity > max {
			max = t.Quantity
		}
	}
	return max
}

// dateRangeLabel renders "26 Sep – 2 Oct 2026", collapsing the repeated month
// or year when both ends share it.
func dateRangeLabel(from, to time.Time) string {
	const dayMonth = "2 Jan"
	const full = "2 Jan 2006"

	switch {
	case from.Year() == to.Year() && from.Month() == to.Month():
		if from.Day() == to.Day() {
			return to.Format(full)
		}
		return fmt.Sprintf("%d – %s", from.Day(), to.Format(full))
	case from.Year() == to.Year():
		return fmt.Sprintf("%s – %s", from.Format(dayMonth), to.Format(full))
	default:
		return fmt.Sprintf("%s – %s", from.Format(dayMonth), to.Format(full))
	}
}
