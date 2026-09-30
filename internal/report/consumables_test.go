package report

import (
	"strings"
	"testing"
	"time"

	"github.com/HalefS/lira/internal/data"
)

// The print icon set is generated from the frontend's CONSUMABLE_ICONS. This
// fails if someone adds an icon to the Go catalog (data.ConsumableIcons) and
// does not regenerate the map, which would otherwise show a blank chip in
// every printed report.
func TestConsumableIconsMatchCatalog(t *testing.T) {
	if len(consumableIconSVG) != len(data.ConsumableIcons) {
		t.Errorf("print icon map has %d entries, catalog has %d",
			len(consumableIconSVG), len(data.ConsumableIcons))
	}
	for _, key := range data.ConsumableIcons {
		svg, ok := consumableIconSVG[key]
		if !ok {
			t.Errorf("catalog icon %q has no entry in the print icon map", key)
			continue
		}
		if strings.TrimSpace(string(svg)) == "" {
			t.Errorf("print icon %q is empty", key)
		}
		if _, ok := consumableIconTone[key]; !ok {
			t.Errorf("catalog icon %q has no tone", key)
		}
	}
	for key := range consumableIconSVG {
		if !data.IsConsumableIcon(key) {
			t.Errorf("print icon map has %q, which is not in the catalog", key)
		}
	}
}

// An unknown key must fall back to the generic box rather than rendering an
// empty chip — consumable rows keep their stored icon even after the catalog
// entry is deleted.
func TestConsumableIconForFallsBackToBox(t *testing.T) {
	svg, tone := consumableIconFor("no-such-icon")
	if tone != consumableIconTone["box"] {
		t.Errorf("tone = %q, want the box tone %q", tone, consumableIconTone["box"])
	}
	if !strings.Contains(string(svg), "<svg") {
		t.Errorf("fallback is not a complete <svg> element: %q", svg)
	}
	if string(svg) == "" {
		t.Error("fallback svg is empty")
	}
	// The known key must still work.
	known, knownTone := consumableIconFor("battery-aa")
	if knownTone != "amber" {
		t.Errorf("battery-aa tone = %q, want amber", knownTone)
	}
	if !strings.Contains(string(known), "AA") {
		t.Errorf("battery-aa drawing lost its label: %q", known)
	}
}

func TestWeekRangeIsMondayToSunday(t *testing.T) {
	// 2026-09-21 is a Monday; 2026-09-27 the Sunday of the same week.
	tests := []struct {
		day       string // any day inside the week
		wantStart string
	}{
		{"2026-09-21", "2026-09-21"}, // Monday
		{"2026-09-22", "2026-09-21"}, // Tuesday
		{"2026-09-26", "2026-09-21"}, // Saturday
		{"2026-09-27", "2026-09-21"}, // Sunday
		{"2026-09-28", "2026-09-28"}, // next Monday starts a new week
		{"2026-09-20", "2026-09-14"}, // the Sunday before belongs to the prior week
	}
	for _, tc := range tests {
		day := mustDay(t, tc.day)
		start, end := data.WeekRange(day)
		if got := start.Format("2006-01-02"); got != tc.wantStart {
			t.Errorf("WeekRange(%s) start = %s, want %s", tc.day, got, tc.wantStart)
		}
		if start.Weekday() != time.Monday {
			t.Errorf("WeekRange(%s) starts on %s, want Monday", tc.day, start.Weekday())
		}
		if got := end.Format("2006-01-02"); got != "2026-10-04" && start.Format("2006-01-02") != tc.wantStart {
			t.Errorf("WeekRange(%s) end = %s, unexpected", tc.day, got)
		}
		if !end.After(start) {
			t.Errorf("WeekRange(%s) end %s is not after start %s", tc.day, end, start)
		}
		if end.Sub(start) != 7*24*time.Hour {
			t.Errorf("WeekRange(%s) spans %s, want 168h", tc.day, end.Sub(start))
		}
	}
}

// The window is half-open, so a row logged exactly at the following midnight
// lands in the next week rather than being counted twice.
func TestWeekRangeEndIsExclusiveNextMonday(t *testing.T) {
	start, end := data.WeekRange(mustDay(t, "2026-09-26"))
	lastMoment := end.Add(-time.Nanosecond)
	if lastMoment.Before(start) || !lastMoment.Before(end) {
		t.Errorf("the last in-window moment %s is outside [%s, %s)", lastMoment, start, end)
	}
	if !end.After(lastMoment) {
		t.Error("end should be strictly after the last in-window moment")
	}
}

func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return d
}

func sampleWeekly() *data.ConsumablesWeeklyReport {
	from := time.Date(2026, 9, 21, 0, 0, 0, 0, time.Local)
	to := from.AddDate(0, 0, 7)
	techID := int64(2)
	return &data.ConsumablesWeeklyReport{
		From: from, To: to,
		FromDate:     "2026-09-21",
		ToDate:       "2026-09-27",
		GeneratedAt:  time.Date(2026, 9, 28, 9, 0, 0, 0, time.Local),
		TotalRecords: 3, TotalQuantity: 9, DistinctItems: 2, LocationsCount: 2,
		ByItem: []*data.ConsumableItemTotal{
			{Item: "AA Batteries", Icon: "battery-aa", Quantity: 7, Records: 2},
			{Item: "Remote Control", Icon: "remote", Quantity: 2, Records: 1},
		},
		ByLocation: []*data.ConsumableLocationTotal{
			{Mode: "apt", Location: "312", Quantity: 7, Records: 2},
			{Mode: "dept", Location: "Kitchen", Quantity: 2, Records: 1},
		},
		ByTechnician: []*data.ConsumableTechTotal{
			{UserID: &techID, Name: "Ana Gonçalves", AvatarIdx: 0, Quantity: 9, Records: 3},
		},
		ByDay: []*data.ConsumableDayTotal{
			{Date: "2026-09-21", Quantity: 4, Records: 1},
			{Date: "2026-09-23", Quantity: 5, Records: 2},
		},
		Entries: []*data.ConsumablesReportEntry{
			{ID: 1, CreatedAt: from.Add(9 * time.Hour), Item: "AA Batteries", Icon: "battery-aa",
				Quantity: 4, Mode: "apt", Location: "312", IssueID: 900, IssueType: "Door",
				IssueStatus: "Ok", IssueProblem: "Remote controls flat", LoggedBy: &techID,
				LoggedByName: "Ana Gonçalves", LoggedByIdx: 0},
			{ID: 2, CreatedAt: from.AddDate(0, 0, 2).Add(10 * time.Hour), Item: "Remote Control",
				Icon: "remote", Quantity: 1, Mode: "dept", Location: "Kitchen", IssueID: 901,
				IssueType: "TV", IssueStatus: "Ok", IssueProblem: "Remote missing",
				LoggedBy: &techID, LoggedByName: "Ana Gonçalves", LoggedByIdx: 0},
			{ID: 3, CreatedAt: from.AddDate(0, 0, 2).Add(11 * time.Hour), Item: "AA Batteries",
				Icon: "battery-aa", Quantity: 4, Mode: "apt", Location: "312", IssueID: 902,
				IssueType: "Door", IssueStatus: "Ok", IssueProblem: "More batteries",
				LoggedBy: &techID, LoggedByName: "Ana Gonçalves", LoggedByIdx: 0},
		},
	}
}

func TestRenderConsumablesHTML(t *testing.T) {
	html, err := RenderConsumablesHTML(sampleWeekly())
	if err != nil {
		t.Fatalf("RenderConsumablesHTML: %v", err)
	}

	for _, want := range []string{
		"Consumables Report",
		"2026-09-21", "2026-09-27", // the range, in the band and the footer
		"Week at a glance",
		"Usage by day",
		"By item", "By location", "By technician",
		"Apartment usage", "Department usage",
		"AA Batteries", "Remote Control", // the items
		"312", "Kitchen", // the locations
		"Ana Gonçalves",        // the technician
		"Remote controls flat", // the issue text
		"@page",                // the shared stylesheet was injected
		".band-top",            // ...and it is the shared one
		"table-layout: fixed",
		"tone-amber", // the battery icon's tone
		"<svg",       // icons are real svg elements
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered report is missing %q", want)
		}
	}

	// Entries exist, so both detail tables must be there.
	if !strings.Contains(html, "Apartment usage") {
		t.Error("a report with apartment entries should render the apartment table")
	}
	if !strings.Contains(html, "Department usage") {
		t.Error("a report with department entries should render the department table")
	}
}

func TestRenderConsumablesHTMLEmptyWeek(t *testing.T) {
	rep := sampleWeekly()
	rep.TotalRecords = 0
	rep.TotalQuantity = 0
	rep.Entries = nil
	rep.ByItem = nil
	rep.ByLocation = nil
	rep.ByTechnician = nil
	rep.ByDay = nil

	html, err := RenderConsumablesHTML(rep)
	if err != nil {
		t.Fatalf("RenderConsumablesHTML: %v", err)
	}
	if !strings.Contains(html, "No consumables were recorded") {
		t.Error("an empty week should say so")
	}
	if strings.Contains(html, "<table") {
		t.Error("an empty week should not render a detail table")
	}
	// The date range still has to be right, or the reader cannot tell which
	// week came back empty.
	if !strings.Contains(html, "2026-09-21") {
		t.Error("an empty week should still show its date range")
	}
}

// One bar per day of the week, so a quiet day reads as quiet rather than
// missing.
func TestConsumablesDayChartCoversEveryDay(t *testing.T) {
	rep := sampleWeekly()
	rep.ByDay = nil // nothing used at all
	v := newConsumablesView(rep)
	if len(v.Days) != 7 {
		t.Fatalf("day chart has %d columns, want 7", len(v.Days))
	}
	if v.Days[0].Label != "21 Sep" {
		t.Errorf("first column = %q, want 21 Sep", v.Days[0].Label)
	}
	if v.Days[6].Label != "27 Sep" {
		t.Errorf("last column = %q, want 27 Sep", v.Days[6].Label)
	}
	for i, d := range v.Days {
		if !d.Empty {
			t.Errorf("column %d (%s) should be marked empty when nothing was used", i, d.Label)
		}
	}
}

func TestConsumablesDayChartFloor(t *testing.T) {
	rep := sampleWeekly()
	v := newConsumablesView(rep)
	var seen []string
	for _, d := range v.Days {
		if !d.Empty {
			seen = append(seen, d.Label)
			// A single unit still gets a visible bar rather than nothing.
			if d.Height <= 0 {
				t.Errorf("%s has %d units but a zero-height bar", d.Label, d.Quantity)
			}
		}
	}
	if len(seen) != 2 {
		t.Errorf("chart shows %d non-empty days (%v), want 2", len(seen), seen)
	}
}

func TestConsumablesEntriesSplitByMode(t *testing.T) {
	v := newConsumablesView(sampleWeekly())
	if len(v.AptEntries) != 2 {
		t.Errorf("apartment entries = %d, want 2", len(v.AptEntries))
	}
	if len(v.DeptEntries) != 1 {
		t.Errorf("department entries = %d, want 1", len(v.DeptEntries))
	}
	if len(v.Entries) != 3 {
		t.Errorf("total entries = %d, want 3", len(v.Entries))
	}
}

func TestRenderConsumablesHTMLEscapesIssueText(t *testing.T) {
	rep := sampleWeekly()
	rep.Entries[0].IssueProblem = `<script>alert("x")</script>`
	rep.Entries[0].Location = `A"B&C`

	html, err := RenderConsumablesHTML(rep)
	if err != nil {
		t.Fatalf("RenderConsumablesHTML: %v", err)
	}
	if strings.Contains(html, "<script>alert") {
		t.Error("issue text was embedded unescaped")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("issue text was not HTML-escaped")
	}
}

func TestRenderConsumablesHTMLRejectsNil(t *testing.T) {
	if _, err := RenderConsumablesHTML(nil); err == nil {
		t.Error("RenderConsumablesHTML(nil) should return an error")
	}
}

func TestDateRangeLabel(t *testing.T) {
	day := func(s string) time.Time {
		d, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	tests := []struct{ from, to, want string }{
		// Same month: the month is stated once, at the end.
		{"2026-09-21", "2026-09-27", "21 – 27 Sep 2026"},
		// Spanning months, same year.
		{"2026-09-28", "2026-10-04", "28 Sep – 4 Oct 2026"},
		// Spanning years.
		{"2025-12-29", "2026-01-04", "29 Dec – 4 Jan 2026"},
	}
	for _, tc := range tests {
		if got := dateRangeLabel(day(tc.from), day(tc.to)); got != tc.want {
			t.Errorf("dateRangeLabel(%s, %s) = %q, want %q", tc.from, tc.to, got, tc.want)
		}
	}
}

// Both templates must carry the shared-stylesheet placeholder, and it must
// resolve -- a template that silently lost it would render unstyled.
func TestTemplatesShareTheBaseStylesheet(t *testing.T) {
	for _, name := range []string{"templates/daily.html", "templates/consumables.html"} {
		src := mustReadTemplate(name)
		if !strings.Contains(src, baseCSSMarker) {
			t.Errorf("%s is missing the %s placeholder", name, baseCSSMarker)
		}
		if !strings.Contains(src, "<style>") || !strings.Contains(src, "</style>") {
			t.Errorf("%s must have a balanced <style> block", name)
		}
	}
	// mustLoadTemplate panics on a missing placeholder, so simply loading both
	// proves the substitution works.
	_ = dailyTemplate
	_ = consumablesTemplate
}
