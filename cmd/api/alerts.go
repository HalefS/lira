package main

import (
	"net/http"

	"github.com/HalefS/lira/internal/data"
)

// listAlertsHandler powers the Alerts page: every (mode, location, type)
// combination that has happened 2+ times within the manager-configured
// window, along with each occurrence's details.
func (app *application) listAlertsHandler(w http.ResponseWriter, r *http.Request) {
	settings, err := app.models.Settings.Get()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	groups, err := app.models.Issues.GetRecurringGroups(settings.DuplicateWindowHours)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if groups == nil {
		groups = []*data.RecurringGroup{}
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"window_hours": settings.DuplicateWindowHours,
		"alerts":       groups,
	}, nil)
}
