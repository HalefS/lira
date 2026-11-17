package report

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HalefS/lira/internal/data"
)

func supportFixture() *data.SupportReport {
	reported := time.Date(2026, time.November, 10, 9, 15, 0, 0, time.UTC)
	solved := reported.Add(90 * time.Minute)
	duration := 90

	return &data.SupportReport{
		From:        "2026-11-01",
		To:          "2026-11-17",
		GeneratedAt: time.Date(2026, time.November, 17, 14, 30, 0, 0, time.UTC),
		Stats: &data.SupportStats{
			From: "2026-11-01",
			To:   "2026-11-17",
			ByCompany: []*data.SupportCompanyStats{
				{Company: "telnet", Total: 1, Resolved: 1, Timed: 1, AvgMinutes: 90, MedianMinutes: 90, FastestMinutes: 90, SlowestMinutes: 90},
				{Company: "telefonica", Total: 1, Pending: 1},
			},
			Overall: &data.SupportCompanyStats{
				Total: 2, Resolved: 1, Pending: 1, Timed: 1,
				AvgMinutes: 90, MedianMinutes: 90, FastestMinutes: 90, SlowestMinutes: 90,
			},
		},
		Rows: []*data.SupportListRow{
			{
				ID: 1, Company: "telnet", Status: data.SupportStatusSolved,
				StartedAt: &reported, ResolvedAt: &solved, DurationMinutes: &duration,
				Reference: "Ana", IssueID: 42, IssueLocation: "1301",
				IssueProblem: "Door will not close", Notes: "Called back",
			},
			{
				ID: 2, Company: "telefonica", Status: data.SupportStatusPending,
				IssueID: 43, IssueLocation: "1204",
				IssueProblem: "<script>alert(1)</script>", Reference: "2026-88123",
			},
		},
	}
}

func TestRenderSupportHTML(t *testing.T) {
	html, err := RenderSupportHTML(supportFixture())
	if err != nil {
		t.Fatalf("RenderSupportHTML: %v", err)
	}

	for _, want := range []string{
		"Third-party Support",
		"By company", "Telnet", "Telefónica", "Both companies",
		"Summary", "Avg. turnaround", "1h 30m",
		"Handovers <span class=\"count\">(2)</span>",
		"1301", "Door will not close", "2026-88123",
		"OK", "Pending", "Called back",
		"17 Nov 2026, 14:30",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered report is missing %q", want)
		}
	}

	// A handover's notes and problem are user text: they must be escaped, not
	// interpolated into the document as markup.
	if strings.Contains(html, "<script>") {
		t.Error("rendered report contains raw user markup")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("rendered report does not escape user markup")
	}

	// The shared stylesheet is substituted into the template source before it is
	// parsed; neither the marker nor any unresolved action may survive.
	if strings.Contains(html, baseCSSMarker) || strings.Contains(html, "{{") {
		t.Error("rendered report contains an unresolved template action")
	}
}

// The template has to survive the real print pipeline, not just html/template:
// the shared bands and the inline accent colours are where a report can gain a
// blank trailing page or lose its table header on a continuation page.
func TestSupportReportPrints(t *testing.T) {
	browser := requireBrowser(t)

	html, err := RenderSupportHTML(supportFixture())
	if err != nil {
		t.Fatalf("RenderSupportHTML: %v", err)
	}
	pdf, err := browser.Render(context.Background(), html)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if pages := pageCount(pdf); pages < 1 {
		t.Errorf("support report rendered as %d pages, want at least 1", pages)
	}
}

func TestRenderSupportHTMLRejectsBadInput(t *testing.T) {
	if _, err := RenderSupportHTML(nil); err == nil {
		t.Error("a nil support report should be rejected")
	}
	empty := supportFixture()
	empty.Stats = nil
	if _, err := RenderSupportHTML(empty); err == nil {
		t.Error("a support report without stats should be rejected")
	}
}

func TestRenderSupportHTMLEmpty(t *testing.T) {
	r := supportFixture()
	r.Rows = nil
	r.Stats.Overall = &data.SupportCompanyStats{}

	html, err := RenderSupportHTML(r)
	if err != nil {
		t.Fatalf("RenderSupportHTML: %v", err)
	}
	if !strings.Contains(html, "No third-party handovers were logged in this period.") {
		t.Error("an empty report should say so")
	}
	if strings.Contains(html, "<table>") {
		t.Error("an empty report should not render an empty detail table")
	}
}

func TestSupportDuration(t *testing.T) {
	tests := []struct {
		minutes float64
		want    string
	}{
		{0, "0m"},
		{1, "1m"},
		{5, "5m"},
		{60, "1h"},
		{90, "1h 30m"},
		{1440, "1d"},
		{1500, "1d 1h"},
	}
	for _, tc := range tests {
		if got := supportDuration(tc.minutes); got != tc.want {
			t.Errorf("supportDuration(%v) = %q, want %q", tc.minutes, got, tc.want)
		}
	}
}

func TestSupportRangeLabel(t *testing.T) {
	tests := []struct{ from, to, want string }{
		{"", "", "All time"},
		{"2026-11-17", "", "From 17 Nov 2026"},
		{"", "2026-11-17", "Up to 17 Nov 2026"},
		{"2026-11-01", "2026-11-17", "1 – 17 Nov 2026"},
	}
	for _, tc := range tests {
		if got := supportRangeLabel(tc.from, tc.to); got != tc.want {
			t.Errorf("supportRangeLabel(%q, %q) = %q, want %q", tc.from, tc.to, got, tc.want)
		}
	}
}
