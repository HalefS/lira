package main

import (
	"errors"
	"net/http"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// listAlertsHandler powers the Alerts page. It first syncs the tracked
// recurring_alerts table against what's currently recurring in the issues
// table (creating new pending alerts, reopening ones that recurred again
// after being marked solved), then returns the list — optionally filtered
// by ?status=pending|solved — plus a pending count for the sidebar badge.
func (app *application) listAlertsHandler(w http.ResponseWriter, r *http.Request) {
	settings, err := app.models.Settings.Get()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.models.RecurringAlerts.Sync(settings.DuplicateWindowHours); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	status := app.readString(r.URL.Query(), "status", "")

	alerts, err := app.models.RecurringAlerts.List(status)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if alerts == nil {
		alerts = []*data.RecurringAlert{}
	}

	pendingCount, err := app.models.RecurringAlerts.CountPending()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"window_hours":  settings.DuplicateWindowHours,
		"alerts":        alerts,
		"pending_count": pendingCount,
	}, nil)
}

// solveAlertHandler marks a recurring alert as solved, recording the final
// solution and who resolved it. Managers only.
func (app *application) solveAlertHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	var input struct {
		Solution string `json:"solution"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	if data.ValidateRecurringAlertSolution(v, input.Solution); !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)

	alert, err := app.models.RecurringAlerts.MarkSolved(id, input.Solution, user.ID)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"alert": alert}, nil)
}

// deleteAlertHandler removes a recurring alert entirely. Managers only.
func (app *application) deleteAlertHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.RecurringAlerts.Delete(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "alert deleted"}, nil)
}
