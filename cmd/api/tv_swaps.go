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

// listTVSwapsHandler returns every recorded TV swap, newest first.
//
// Read-only for every authenticated user, like /v1/consumables: swaps are
// edited through the issue they belong to, which keeps the owner-or-manager
// permission check in one place. The `issue_id` filter is what a future
// "TV movements" page would use to scope itself to one issue.
func (app *application) listTVSwapsHandler(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	issueID := int64(app.readInt(qs, "issue_id", 0))
	limit := app.readInt(qs, "limit", 200)
	if limit < 1 || limit > 1000 {
		limit = 200
	}

	swaps, err := app.models.TVSwaps.GetList(issueID, limit)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"swaps": swaps}, nil)
}
