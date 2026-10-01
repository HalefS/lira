package main

import (
	"errors"
	"net/http"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// validateTVSwaps checks the TV swaps sent with an issue create/update request.
//
// It runs in two steps, mirroring validateSupportRequests:
//  1. shape — both ends of every swap present and short enough, no self-swap,
//     no duplicate move (data.ValidateTVSwaps);
//  2. normalization a browser form can't be trusted to have done: whitespace
//     trimmed off the rooms and the note.
//
// Note there is no cross-check against the issue's own type here. A swap only
// makes sense on a type flagged tracks_swaps, but the flag lives on the
// issue_types row and can be turned off after swaps were recorded — refusing
// then would make existing issues unsaveable. The prompt is driven by the flag;
// the data is not policed by it.
func (app *application) validateTVSwaps(v *validator.Validator, uses []data.TVSwapUse) {
	shape := validator.New()
	data.ValidateTVSwaps(shape, uses)
	for key, msg := range shape.Errors {
		v.AddError(key, msg)
	}
}

// setIssueTVSwaps stores an issue's swaps, writing the error response itself
// and reporting whether it did.
//
// A swap that vanished while the form was open comes back as a validation error
// rather than a 500: it is something the user fixes by reopening the issue, and
// saying so is more use than an internal error.
func (app *application) setIssueTVSwaps(w http.ResponseWriter, r *http.Request, issueID int64, uses []data.TVSwapUse, loggedBy int64) error {
	err := app.models.TVSwaps.SetForIssue(issueID, uses, loggedBy)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, data.ErrRecordNotFound):
		v := validator.New()
		v.AddError("tv_swaps", "one of the TV swaps no longer exists on this issue — reopen the issue and try again")
		app.failedValidationResponse(w, r, v.Errors)
	default:
		app.serverErrorResponse(w, r, err)
	}
	return err
}

// listTVSwapsHandler powers the TV swaps page: every recorded swap, newest
// first, optionally narrowed to one date or a from/to range.
//
// Read-only for every authenticated user, like /v1/consumables: swaps are
// edited through the issue they belong to, which keeps the owner-or-manager
// permission check in one place.
func (app *application) listTVSwapsHandler(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	issueID := int64(app.readInt(qs, "issue_id", 0))
	date := app.readString(qs, "date", "")
	from := app.readString(qs, "from", "")
	to := app.readString(qs, "to", "")

	// The single-date filter is shorthand for a one-day range, so the page can
	// offer "Today" and "All dates" as buttons over the same underlying range.
	if date != "" {
		from, to = date, date
	}

	v := validator.New()
	for field, value := range map[string]string{"date": date, "from": from, "to": to} {
		if value != "" {
			v.Check(datePattern.MatchString(value), field, "must be a date in YYYY-MM-DD format")
		}
	}
	if from != "" && to != "" {
		v.Check(from <= to, "from", "must be on or before the end date")
	}
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	limit := app.readInt(qs, "limit", 500)
	if limit < 1 || limit > 1000 {
		limit = 500
	}

	swaps, err := app.models.TVSwaps.GetList(issueID, from, to, limit)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"swaps": swaps}, nil)
}
