package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// solveTVSwapAlertHandler records that the faulty set behind a swap was dealt
// with, along with how. Managers only, matching the recurring alerts.
//
// The verdict is stamped against the swap's current version, so a later edit to
// the swap re-opens the alert rather than leaving a "solved" stamp on a row that
// no longer says what it said when it was signed off.
func (app *application) solveTVSwapAlertHandler(w http.ResponseWriter, r *http.Request) {
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

	// Trimmed before it is validated and before it is stored, so what the card
	// shows afterwards is exactly what was checked. A whitespace-only note is
	// refused rather than recorded as an empty solution.
	solution := strings.TrimSpace(input.Solution)

	v := validator.New()
	data.ValidateTVSwapAlertSolution(v, solution)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)

	alert, err := app.models.TVSwapAlerts.MarkSolved(id, solution, user.ID)
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

// deleteTVSwapAlertHandler removes a TV swap alert without touching the swap
// underneath it. The swap is the record that a set was carried and stays put
// whatever anyone thinks of the alert; this is for an alert raised by mistake.
// Managers only.
//
// This is mostly unreachable in normal use, because Sync recreates a pending
// alert for any live swap that has none. It earns its keep when an alert needs to
// go before the fault does.
func (app *application) deleteTVSwapAlertHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.TVSwapAlerts.Delete(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "tv swap alert deleted"}, nil)
}
