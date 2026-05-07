package main

import (
	"net/http"
)

// GET /v1/reports/daily?date=YYYY-MM-DD
func (app *application) dailyReportHandler(w http.ResponseWriter, r *http.Request) {
	date := app.readString(r.URL.Query(), "date", "")

	report, err := app.models.Issues.GetDailyReport(date)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"report": report}, nil)
}
