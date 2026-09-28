package main

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/HalefS/lira/internal/report"
)

// dateParamPattern matches the "YYYY-MM-DD" the report endpoints accept. It is
// checked here so a malformed date is refused with a message naming the
// parameter, rather than being handed to the database as an internal error.
var dateParamPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// GET /v1/reports/daily?date=YYYY-MM-DD
func (app *application) dailyReportHandler(w http.ResponseWriter, r *http.Request) {
	date := app.readString(r.URL.Query(), "date", "")

	rep, err := app.models.Issues.GetDailyReport(date)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"report": rep}, nil)
}

// GET /v1/reports/daily.pdf?date=YYYY-MM-DD
//
// The same report, printed. It is laid out as HTML and rendered by a headless
// Chromium (see internal/report), so the page is styled with the
// application's own palette and typography instead of a second, separately
// maintained copy of them.
//
// Readable by any authenticated user, matching the JSON endpoint above.
func (app *application) dailyReportPDFHandler(w http.ResponseWriter, r *http.Request) {
	date := app.readString(r.URL.Query(), "date", "")
	if date != "" && !dateParamPattern.MatchString(date) {
		app.errorResponse(w, r, http.StatusBadRequest, "date must be in YYYY-MM-DD format")
		return
	}

	// A missing browser is a deployment problem, not a bad request, and the
	// rest of the application works without one — so it is reported as
	// "not implemented" rather than taking the server down at boot.
	if app.browser == nil {
		reason := "no chromium-based browser was found on this server"
		if app.browserErr != nil && !errors.Is(app.browserErr, report.ErrNoBrowser) {
			// The operator named a browser explicitly and it did not resolve,
			// so say that rather than sending them hunting for a missing install.
			reason = "the configured pdf report browser could not be used: " + app.browserErr.Error()
		}
		app.errorResponse(w, r, http.StatusNotImplemented, "pdf reports are unavailable: "+reason)
		return
	}

	rep, err := app.models.Issues.GetDailyReport(date)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	html, err := report.RenderDailyHTML(rep)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// The request context bounds the browser run, so a client that gives up
	// doesn't leave a Chrome process behind.
	pdf, err := app.browser.Render(r.Context(), html)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", contentDisposition("attachment",
		fmt.Sprintf("LIRA-Daily-Report-%s.pdf", rep.Date)))
	// The document is generated per request from a date the user can change and
	// come back to, so it must never be reused for a different day.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
	w.WriteHeader(http.StatusOK)
	w.Write(pdf)
}

// contentDisposition builds a Content-Disposition header value. The filename is
// assembled from a fixed prefix and a date the handler has already matched
// against dateParamPattern, but it is still stripped of quotes and line breaks
// so it cannot break out of the quoted string.
func contentDisposition(disposition, filename string) string {
	filename = strings.NewReplacer(`"`, "", "\\", "", "\r", "", "\n", "").Replace(filename)
	return fmt.Sprintf(`%s; filename="%s"`, disposition, filename)
}
