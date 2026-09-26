package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HalefS/lira/internal/report"
)

func TestDateParamPattern(t *testing.T) {
	valid := []string{"2026-09-26", "2026-01-01", "1999-12-31"}
	invalid := []string{"", "26-09-2026", "2026/09/26", "2026-9-26", "yesterday", "2026-09-26; DROP TABLE issues"}
	for _, s := range valid {
		if !dateParamPattern.MatchString(s) {
			t.Errorf("dateParamPattern should accept %q", s)
		}
	}
	for _, s := range invalid {
		if dateParamPattern.MatchString(s) {
			t.Errorf("dateParamPattern should reject %q", s)
		}
	}
}

func TestContentDisposition(t *testing.T) {
	tests := []struct{ in, want string }{
		{`LIRA-Daily-Report-2026-09-26.pdf`, `attachment; filename="LIRA-Daily-Report-2026-09-26.pdf"`},
		// Anything that could terminate the quoted string early is stripped,
		// so a crafted filename cannot inject extra header parameters.
		{`a"b.pdf`, `attachment; filename="ab.pdf"`},
		{"a\r\nX-Evil: 1.pdf", `attachment; filename="aX-Evil: 1.pdf"`},
		{`a\b.pdf`, `attachment; filename="ab.pdf"`},
	}
	for _, tc := range tests {
		if got := contentDisposition("attachment", tc.in); got != tc.want {
			t.Errorf("contentDisposition(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The PDF endpoint's guard clauses run before any query, so they are exercised
// here against a zero-value application rather than a live database.
func TestDailyReportPDFRejectsMalformedDate(t *testing.T) {
	app := &application{}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/reports/daily.pdf?date=not-a-date", nil)
	app.dailyReportPDFHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "YYYY-MM-DD") {
		t.Errorf("body = %q, want it to name the expected date format", w.Body.String())
	}
}

// A missing browser is a deployment problem, and the rest of the app must keep
// working without one, so this must not be a 500.
func TestDailyReportPDFReportsMissingBrowser(t *testing.T) {
	app := &application{browserErr: report.ErrNoBrowser}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/reports/daily.pdf?date=2026-09-26", nil)
	app.dailyReportPDFHandler(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if !strings.Contains(w.Body.String(), "chromium") {
		t.Errorf("body = %q, want it to explain that no browser was found", w.Body.String())
	}
}

// A browser that was named explicitly but did not resolve must report that,
// not send the operator looking for an install that is already there.
func TestDailyReportPDFReportsBadConfiguredBrowser(t *testing.T) {
	app := &application{browserErr: errors.New(`browser "C:\nope\chrome.exe": no such file`)}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/reports/daily.pdf?date=2026-09-26", nil)
	app.dailyReportPDFHandler(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if !strings.Contains(w.Body.String(), "configured pdf report browser") {
		t.Errorf("body = %q, want it to blame the configured path", w.Body.String())
	}
}
