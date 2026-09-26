package report

import (
	"strings"
	"testing"
	"time"

	"github.com/HalefS/lira/internal/data"
)

func TestTypeColorPrefersStoredHex(t *testing.T) {
	colors := map[string]string{"Door": "#123456", "Internet": "#ABCDEF"}
	tests := []struct {
		name string
		want string
	}{
		{"Door", "#123456"},
		{"Internet", "#ABCDEF"},
	}
	for _, tc := range tests {
		if got := typeColor(tc.name, colors); got != tc.want {
			t.Errorf("typeColor(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Rows created before issue-type colors became editable (migration 000009)
// hold CSS class names, and nothing rewrites them until a manager opens the
// color picker. Those must fall back rather than be emitted as a color.
func TestTypeColorFallsBackForLegacyAndUnknown(t *testing.T) {
	colors := map[string]string{
		"Door":     "badge-door",
		"Internet": "#1D9E75",
		"TV":       "",
		"AC":       "not-a-color",
	}
	tests := []struct{ name, want string }{
		{"Door", "#1769AA"},          // legacy class -> the built-in Door color
		{"Internet", "#1D9E75"},      // valid hex -> used as-is
		{"TV", "#9D174D"},            // empty -> built-in
		{"AC", "#3730A3"},            // garbage -> built-in
		{"Boiler", unknownTypeColor}, // neither stored nor built-in
	}
	for _, tc := range tests {
		if got := typeColor(tc.name, colors); got != tc.want {
			t.Errorf("typeColor(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestIsHexColor(t *testing.T) {
	valid := []string{"#000000", "#ffffff", "#1769AA", "#1d9e75"}
	invalid := []string{"", "#fff", "1769AA", "#1769A", "#1769AAG", "badge-door", "#17 69AA"}
	for _, s := range valid {
		if !isHexColor(s) {
			t.Errorf("isHexColor(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if isHexColor(s) {
			t.Errorf("isHexColor(%q) = true, want false", s)
		}
	}
}

func TestInitials(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Ana Gonçalves", "AG"},
		{"Carla Menezes", "CM"},
		{"Cher", "C"},
		{"  Bruno   Sá  ", "BS"},
		{"Pedro Maria Silva Costa", "PM"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := initials(tc.in); got != tc.want {
			t.Errorf("initials(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// avatarColors indexes a fixed palette, so a large or negative avatar_idx must
// wrap rather than panic — it comes straight from the database.
func TestAvatarColorsWraps(t *testing.T) {
	for _, idx := range []int{-3, -1, 0, 1, 4, 5, 12} {
		bg, fg := avatarColors(idx)
		if bg == "" || fg == "" {
			t.Errorf("avatarColors(%d) = (%q, %q), want both set", idx, bg, fg)
		}
	}
	// The palette is five entries, so index 5 must equal index 0.
	five, _ := avatarColors(5)
	zero, _ := avatarColors(0)
	if five != zero {
		t.Errorf("avatarColors(5) = %q, want it to wrap to avatarColors(0) = %q", five, zero)
	}
}

func TestStatusClass(t *testing.T) {
	if got := statusClass("Ok"); got != "ok" {
		t.Errorf("statusClass(\"Ok\") = %q, want \"ok\"", got)
	}
	if got := statusClass("Pending"); got != "pending" {
		t.Errorf("statusClass(\"Pending\") = %q, want \"pending\"", got)
	}
}

func TestOrDash(t *testing.T) {
	if got := orDash("   "); got != "—" {
		t.Errorf("orDash(blank) = %q, want an em dash", got)
	}
	if got := orDash("replaced"); got != "replaced" {
		t.Errorf("orDash(%q) = %q, want it unchanged", "replaced", got)
	}
}

func TestBarWidthNeverZero(t *testing.T) {
	if got := barWidth(1, 100); got != 2 {
		t.Errorf("barWidth(1, 100) = %v, want the 2%% floor so a real count stays visible", got)
	}
	if got := barWidth(50, 100); got != 50 {
		t.Errorf("barWidth(50, 100) = %v, want 50", got)
	}
	if got := barWidth(5, 0); got != 0 {
		t.Errorf("barWidth with no maximum = %v, want 0", got)
	}
}

// Bars must be ordered by count, with the busiest type and technician first.
func TestChartsAreSortedByCountDesc(t *testing.T) {
	r := &data.DailyReport{
		ByType: map[string]int{"Door": 2, "TV": 9, "AC": 5},
		ByTechnician: []data.TechStat{
			{Name: "Low", Count: 1},
			{Name: "High", Count: 7},
			{Name: "None", Count: 0},
		},
	}
	types := typeBars(r)
	if len(types) != 3 || types[0].Name != "TV" || types[2].Name != "Door" {
		t.Errorf("typeBars order = %v, want TV, AC, Door", names(types))
	}
	techs := techBars(r)
	// The zero-count technician is dropped from a printed report.
	if len(techs) != 2 || techs[0].Name != "High" {
		t.Errorf("techBars = %v, want High first and the empty technician dropped", techNames(techs))
	}
}

func TestTechBarsReportOverflow(t *testing.T) {
	var techs []data.TechStat
	for i := 0; i < maxTechBars+4; i++ {
		techs = append(techs, data.TechStat{Name: "T", Count: i + 1})
	}
	bars := techBars(&data.DailyReport{ByTechnician: techs})
	last := bars[len(bars)-1]
	if !last.IsOverflow || last.Overflow != 4 {
		t.Errorf("overflow row = %+v, want IsOverflow with a count of 4", last)
	}
}

func names(bars []typeBar) []string {
	out := make([]string, len(bars))
	for i, b := range bars {
		out[i] = b.Name
	}
	return out
}

func techNames(bars []techBar) []string {
	out := make([]string, len(bars))
	for i, b := range bars {
		out[i] = b.Name
	}
	return out
}

func sampleReport() *data.DailyReport {
	return &data.DailyReport{
		Date:        "2026-09-26",
		GeneratedAt: time.Date(2026, 9, 26, 18, 42, 0, 0, time.UTC),
		Summary: data.ReportSummary{
			TotalIssues: 3, AptIssues: 2, DeptIssues: 1,
			Resolved: 2, Pending: 1, ResolutionRate: 66.7,
			AvgMinutes: 30, TotalMinutes: 90,
			FastestMinutes: 10, SlowestMinutes: 50, FalsePositives: 1,
		},
		ByType:       map[string]int{"Door": 2, "TV": 1},
		ByMode:       map[string]int{"apt": 2, "dept": 1},
		ByStatus:     map[string]int{"Ok": 2, "Pending": 1},
		TypeColors:   map[string]string{"Door": "#1769AA", "TV": "#9D174D"},
		ByTechnician: []data.TechStat{{UserID: 2, Name: "Ana Gonçalves", AvatarIdx: 0, Count: 2}},
		AptIssues: []*data.Issue{
			{
				ID: 1, Location: "101", Type: "Door",
				Problem: "Reader beeping", Resolution: "Replaced",
				TimeMinutes: 20, Status: "Ok", LoggedByName: "Ana Gonçalves",
			},
			{
				ID: 2, Location: "102", Type: "TV",
				Problem: "No picture", Resolution: "",
				TimeMinutes: 70, Status: "Pending", LoggedByName: "Ana Gonçalves",
				FalsePositive: true,
			},
		},
		DeptIssues: []*data.Issue{
			{ID: 3, Location: "Reception", Type: "Door", Problem: "Bell stuck",
				Resolution: "Cleared", TimeMinutes: 15, Status: "Ok", LoggedByName: "Ana Gonçalves"},
		},
	}
}

func TestRenderDailyHTML(t *testing.T) {
	html, err := RenderDailyHTML(sampleReport())
	if err != nil {
		t.Fatalf("RenderDailyHTML: %v", err)
	}

	for _, want := range []string{
		"LIRA Daily Report",           // document title
		"Saturday, 26 September 2026", // long date, not the raw YYYY-MM-DD
		"@page",                       // print stylesheet present
		"Apartment issues",            // both tables
		"Department issues",
		"Reception",      // a department row made it through
		"101",            // an apartment row
		"FALSE POSITIVE", // the flagged row is marked
		"#1769AA",        // the stored type color
		"2",              // totals reached the template
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML is missing %q", want)
		}
	}

	// An unresolved issue shows an em dash rather than an empty cell.
	if !strings.Contains(html, "—") {
		t.Error("rendered HTML has no em dash for the missing resolution")
	}
}

func TestRenderDailyHTMLEscapesIssueText(t *testing.T) {
	r := sampleReport()
	// Issue text is typed by technicians and must never reach the document as
	// markup — html/template is what guarantees that.
	r.AptIssues[0].Problem = `<script>alert("x")</script>`
	r.AptIssues[0].Location = `A"B&C`

	html, err := RenderDailyHTML(r)
	if err != nil {
		t.Fatalf("RenderDailyHTML: %v", err)
	}
	if strings.Contains(html, "<script>alert") {
		t.Error("issue text was embedded unescaped")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("issue text was not HTML-escaped")
	}
}

func TestRenderDailyHTMLEmptyReport(t *testing.T) {
	html, err := RenderDailyHTML(&data.DailyReport{
		Date:        "2026-09-26",
		GeneratedAt: time.Now(),
		TypeColors:  map[string]string{},
	})
	if err != nil {
		t.Fatalf("RenderDailyHTML: %v", err)
	}
	if !strings.Contains(html, "No issues were logged") {
		t.Error("an empty report should say so instead of rendering empty tables")
	}
	if strings.Contains(html, "<table") {
		t.Error("an empty report should not render an issue table")
	}
}

func TestRenderDailyHTMLRejectsNil(t *testing.T) {
	if _, err := RenderDailyHTML(nil); err == nil {
		t.Error("RenderDailyHTML(nil) should return an error")
	}
}

func TestNewBrowserRejectsBadPath(t *testing.T) {
	if _, err := NewBrowser("no-such-browser-binary-xyz"); err == nil {
		t.Error("NewBrowser should fail when the configured path does not exist")
	}
	if _, err := NewBrowser("."); err == nil {
		t.Error("NewBrowser should fail when the configured path is a directory")
	}
}
