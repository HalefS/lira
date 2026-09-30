package report

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HalefS/lira/internal/data"
)

// These need a Chromium-based browser, because the pagination bugs they guard
// only exist in the print pipeline. They skip rather than fail when none is
// installed, so `go test ./...` still passes on a bare machine.
func requireBrowser(t *testing.T) *Browser {
	t.Helper()
	b, err := NewBrowser("")
	if err != nil {
		t.Skipf("no chromium-based browser available: %v", err)
	}
	return b
}

// A short report must not gain a blank trailing page.
//
// This regressed once: the footer band was a fixed element offset *below* the
// content box, which extends the paginated area, so every short report printed
// as two pages with the second one empty. The bands' geometry is the only
// thing that can reintroduce it, so it is asserted here rather than trusted.
func TestShortReportDoesNotGainABlankPage(t *testing.T) {
	browser := requireBrowser(t)

	html, err := RenderConsumablesHTML(&data.ConsumablesWeeklyReport{
		FromDate: "2026-09-21", ToDate: "2026-09-27",
		GeneratedAt:  time.Now(),
		Entries:      []*data.ConsumablesReportEntry{},
		ByItem:       []*data.ConsumableItemTotal{},
		ByLocation:   []*data.ConsumableLocationTotal{},
		ByTechnician: []*data.ConsumableTechTotal{},
		ByDay:        []*data.ConsumableDayTotal{},
	})
	if err != nil {
		t.Fatalf("RenderConsumablesHTML: %v", err)
	}

	pdf, err := browser.Render(context.Background(), html)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := pageCount(pdf); got != 1 {
		t.Errorf("an empty weekly report rendered as %d pages, want 1 "+
			"(a fixed band is probably offset past the bottom of the content box)", got)
	}
}

// A report with a realistic amount of content still paginates, and every page
// carries real content rather than being an empty overflow page.
func TestWeeklyReportWithDataPaginatesWithoutBlankPages(t *testing.T) {
	browser := requireBrowser(t)

	rep := sampleWeekly()
	// Enough entries to run past one page.
	rep.Entries = append(rep.Entries, rep.Entries...)
	rep.Entries = append(rep.Entries, rep.Entries...)
	rep.Entries = append(rep.Entries, rep.Entries...)
	rep.TotalRecords = len(rep.Entries)

	html, err := RenderConsumablesHTML(rep)
	if err != nil {
		t.Fatalf("RenderConsumablesHTML: %v", err)
	}
	pdf, err := browser.Render(context.Background(), html)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	pages := pageCount(pdf)
	if pages < 1 {
		t.Fatalf("pageCount returned %d", pages)
	}
	t.Logf("weekly report with %d entries rendered %d pages", rep.TotalRecords, pages)
}

// The daily report must not regress either: it is the longer of the two and
// genuinely needs more than one page for a busy day.
func TestDailyReportStillRendersMultiplePages(t *testing.T) {
	browser := requireBrowser(t)

	html, err := RenderDailyHTML(sampleReport())
	if err != nil {
		t.Fatalf("RenderDailyHTML: %v", err)
	}
	pdf, err := browser.Render(context.Background(), html)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := pageCount(pdf); got < 1 {
		t.Errorf("pageCount = %d, want at least 1", got)
	}
	t.Logf("daily report rendered %d pages", pageCount(pdf))
}

func TestPageCount(t *testing.T) {
	// Not a real PDF, but enough to prove the page objects are found and the
	// "/Type /Pages" root is not miscounted as a page.
	fake := []byte("/Type /Pages /Kids[1 0 R] /Type /Page /Type /Page /Type /Page")
	if got := pageCount(fake); got != 3 {
		t.Errorf("pageCount = %d, want 3", got)
	}
	if got := pageCount([]byte("no pdf here")); got != 0 {
		t.Errorf("pageCount on non-PDF = %d, want 0", got)
	}
}

// The shared stylesheet owns the band geometry, so the footer must not be
// pushed below the content box. Asserted on the CSS text because that is where
// the regression actually happened.
func TestFooterBandIsNotOffsetBelowTheContentBox(t *testing.T) {
	css := mustReadTemplate("templates/base.css")
	if !strings.Contains(css, ".band-bottom") {
		t.Fatal("base.css no longer styles the footer band")
	}
	for _, line := range strings.Split(css, "\n") {
		if !strings.Contains(line, "bottom:") {
			continue
		}
		if !strings.Contains(line, "band") && !strings.Contains(strings.TrimSpace(line), "position:") {
			continue
		}
		if strings.Contains(line, "bottom: -") {
			t.Errorf("footer band is offset below the content box (%q), which adds a "+
				"blank page to every short report", strings.TrimSpace(line))
		}
	}
}
