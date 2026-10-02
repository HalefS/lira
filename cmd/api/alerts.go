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
//
// TV swap alerts travel in the same response as tv_swap_alerts. They are a
// separate section on the page rather than a separate page, and they are a
// different kind of thing (one set carried for one room) from a recurring
// issue group, so they stay out of the recurring list and out of its pending
// count: that count is also the sidebar badge and feeds a sentence on the
// Analytics page that says "recurring issues", which would become a lie.
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

	if err := app.models.TVSwapAlerts.Sync(); err != nil {
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

	// Both lists take the same ?status filter, so the two sections stay in step
	// when the page switches between pending and solved.
	tvSwapAlerts, err := app.models.TVSwapAlerts.List(status)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	tvSwapPendingCount, err := app.models.TVSwapAlerts.CountPending()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"window_hours":          settings.DuplicateWindowHours,
		"alerts":                alerts,
		"pending_count":         pendingCount,
		"tv_swap_alerts":        tvSwapAlerts,
		"tv_swap_pending_count": tvSwapPendingCount,
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
