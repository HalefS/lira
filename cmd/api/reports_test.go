package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/report"
)

// A malformed week anchor is refused before any query runs, so it can be
// exercised without a database.
func TestWeeklyConsumablesRejectsMalformedWeek(t *testing.T) {
	app := &application{}

	for _, endpoint := range []string{
		"/v1/reports/consumables/weekly?week=nope",
		"/v1/reports/consumables/weekly.pdf?week=2026-13-45",
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, endpoint, nil)

		switch {
		case strings.HasSuffix(endpoint, ".pdf?week=2026-13-45"):
			app.weeklyConsumablesPDFHandler(w, r)
		default:
			app.weeklyConsumablesReportHandler(w, r)
		}

		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d", endpoint, w.Code, http.StatusBadRequest)
		}
		if !strings.Contains(w.Body.String(), "YYYY-MM-DD") {
			t.Errorf("%s: body = %q, want it to name the expected date format", endpoint, w.Body.String())
		}
	}
}

// A missing browser disables the PDF endpoint but not the JSON one, and
// neither should be a 500.
func TestWeeklyConsumablesPDFReportsMissingBrowser(t *testing.T) {
	app := &application{browserErr: report.ErrNoBrowser}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/reports/consumables/weekly.pdf?week=2026-09-26", nil)
	app.weeklyConsumablesPDFHandler(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if !strings.Contains(w.Body.String(), "chromium") {
		t.Errorf("body = %q, want it to explain that no browser was found", w.Body.String())
	}
}

// An absent anchor means the current week rather than an error.
func TestReportWeekAnchorDefaultsToNow(t *testing.T) {
	app := &application{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/reports/consumables/weekly", nil)

	anchor, ok := app.reportWeekAnchor(w, r)
	if !ok {
		t.Fatal("an absent week anchor should be accepted")
	}
	if anchor.IsZero() {
		t.Error("anchor should default to now, not the zero time")
	}
}

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

// ── The attendance sheet ────────────────────────────────────────────────────
//
// The gate is the interesting part. The rota grid itself is PUBLIC, and the
// attendance sheet carries absence kinds on top of it, so a mistake in the
// middleware chain here would publish sick leave. Both refusal paths are asserted
// against a zero-value application, because requireManager answers before any
// query runs.
func TestRotaAttendancePDFRequiresAManager(t *testing.T) {
	app := &application{browserErr: report.ErrNoBrowser}
	endpoint := "/v1/reports/rota/weekly.pdf?week=2026-10-05"

	// Anonymous: the sentinel optionalAuthentication installs for a missing or
	// unparseable token. It has to go in the context explicitly, because
	// contextGetUser panics rather than returning nil -- the real chain always
	// populates it first.
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, endpoint, nil)
	r = app.contextSetUser(r, data.AnonymousUser)
	app.requireAuth(app.requireManager(app.weeklyRotaPDFHandler))(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// Authenticated but not a manager. This is the one that would have leaked.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, endpoint, nil)
	r = app.contextSetUser(r, &data.User{Role: "technician"})
	app.requireAuth(app.requireManager(app.weeklyRotaPDFHandler))(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("technician: status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// The JSON twin carries the same absences, so it takes the same gate.
func TestRotaAttendanceReportRequiresAManager(t *testing.T) {
	app := &application{}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/reports/rota/weekly?week=2026-10-05", nil)
	r = app.contextSetUser(r, &data.User{Role: "technician"})
	app.requireAuth(app.requireManager(app.weeklyRotaReportHandler))(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// A malformed week anchor is refused before the browser check, so it can be
// exercised without a database.
func TestRotaAttendanceRejectsMalformedWeek(t *testing.T) {
	app := &application{browserErr: report.ErrNoBrowser}

	for _, endpoint := range []string{
		"/v1/reports/rota/weekly?week=nope",
		"/v1/reports/rota/weekly.pdf?week=2026-13-45",
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, endpoint, nil)

		if strings.HasSuffix(endpoint, ".pdf?week=2026-13-45") {
			app.weeklyRotaPDFHandler(w, r)
		} else {
			app.weeklyRotaReportHandler(w, r)
		}

		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d", endpoint, w.Code, http.StatusBadRequest)
		}
		if !strings.Contains(w.Body.String(), "YYYY-MM-DD") {
			t.Errorf("%s: body = %q, want it to name the expected date format", endpoint, w.Body.String())
		}
	}
}

// A missing browser is a deployment problem and must not be a 500.
func TestRotaAttendancePDFReportsMissingBrowser(t *testing.T) {
	app := &application{browserErr: report.ErrNoBrowser}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/reports/rota/weekly.pdf?week=2026-10-05", nil)
	app.weeklyRotaPDFHandler(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
	if !strings.Contains(w.Body.String(), "chromium") {
		t.Errorf("body = %q, want it to explain that no browser was found", w.Body.String())
	}
}

// The workbook carries absence kinds too, so it takes the same manager-only gate.
//
// This endpoint has no browser check, which makes it the one report here where a
// misordered wrapper would not short-circuit anywhere: it would go straight to the
// database and hand out a workbook. That is precisely why it is worth asserting.
func TestRotaAttendanceXLSXRequiresAManager(t *testing.T) {
	app := &application{}
	endpoint := "/v1/reports/rota/weekly.xlsx?week=2026-10-05"
	guarded := app.requireAuth(app.requireManager(app.weeklyRotaXLSXHandler))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, endpoint, nil)
	r = app.contextSetUser(r, data.AnonymousUser)
	guarded(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("anonymous: status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, endpoint, nil)
	r = app.contextSetUser(r, &data.User{Role: "technician"})
	guarded(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("technician: status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

// A malformed week is refused before the model is touched, so this runs without a
// database -- and it must NOT be a 501, which is what a misplaced browser check
// would produce on a machine that happens to lack Chromium.
func TestRotaAttendanceXLSXRejectsMalformedWeek(t *testing.T) {
	app := &application{browserErr: report.ErrNoBrowser}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/v1/reports/rota/weekly.xlsx?week=not-a-week", nil)
	app.weeklyRotaXLSXHandler(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "YYYY-MM-DD") {
		t.Errorf("body = %q, want it to name the expected date format", w.Body.String())
	}
}

// The workbook must be downloadable on a server with no browser at all. That is the
// whole reason this endpoint exists separately from the PDF one, so the asymmetry is
// pinned rather than left as a comment.
//
// A zero-value application has nil models, so a handler that gets as far as the
// query panics -- and that panic IS the proof, because it means the browser gate did
// not fire. What must never happen is a 501, so the status is asserted on the
// recorder with the panic recovered.
func TestRotaAttendanceXLSXNeedsNoBrowser(t *testing.T) {
	app := &application{browserErr: report.ErrNoBrowser}

	w := httptest.NewRecorder()
	func() {
		defer func() { _ = recover() }()
		app.weeklyRotaXLSXHandler(w, httptest.NewRequest(http.MethodGet,
			"/v1/reports/rota/weekly.xlsx?week=2026-10-05", nil))
	}()

	if w.Code == http.StatusNotImplemented {
		t.Error("the workbook endpoint must not require a browser; it returned 501")
	}
}

// The PDF endpoint, given the same missing browser and the same valid week, does
// stop at 501. Asserting both halves is what makes the asymmetry above meaningful:
// without this, "the workbook does not return 501" could just mean the test never got
// far enough to tell.
func TestRotaAttendancePDFDoesStopAt501(t *testing.T) {
	app := &application{browserErr: report.ErrNoBrowser}

	w := httptest.NewRecorder()
	app.weeklyRotaPDFHandler(w, httptest.NewRequest(http.MethodGet,
		"/v1/reports/rota/weekly.pdf?week=2026-10-05", nil))

	if w.Code != http.StatusNotImplemented {
		t.Errorf("the PDF endpoint returned %d, want %d -- the contrast the other test relies on is gone",
			w.Code, http.StatusNotImplemented)
	}
}

// httprouter PANICS at construction time when a static segment sits beside a
// wildcard in the same method tree -- which is why /v1/rota/members exists instead
// of /v1/schedule/roster, per the comment in routes.go. The failure mode is a crash
// at boot, not a bad response, so the only place it can be caught cheaply is a test
// that builds the router.
func TestRoutesBuildWithoutPanicking(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("building the router panicked: %v", rec)
		}
	}()
	app := &application{}
	if r := app.routes(); r == nil {
		t.Error("routes() returned nil")
	}
}
