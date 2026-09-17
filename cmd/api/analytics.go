package main

import "net/http"

// getAnalyticsHandler powers the Analytics page: current-vs-previous
// period comparisons for a rolling week or month.
func (app *application) getAnalyticsHandler(w http.ResponseWriter, r *http.Request) {
	rangeParam := app.readString(r.URL.Query(), "range", "week")

	analytics, err := app.models.Analytics.Get(rangeParam)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"analytics": analytics}, nil)
}
