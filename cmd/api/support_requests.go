package main

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// datePattern matches the "YYYY-MM-DD" the range filters accept. Checked here
// rather than left to the database, where a malformed value would come back as
// an internal error instead of a message saying which filter was wrong.
var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// validateSupportRequests checks the third-party handovers sent with an issue
// create/update request and returns them normalized, ready to store.
//
// It runs in two steps, mirroring validateConsumableUses:
//  1. shape — company, status, the fields each company actually needs, and the
//     two timestamps (data.ValidateSupportRequests);
//  2. normalization a browser form can't be trusted to have done: whitespace
//     trimmed, an empty text field treated as "not provided", the fields that
//     don't belong to the chosen company cleared, and the two timestamps
//     parsed into real times with the turnaround derived from them.
//
// That last part is deliberately forgiving rather than a validation error: a
// Telefonica ticket id left over on a Telnet handover is dropped, not rejected,
// because the database CHECK requires the field to be NULL there and the form
// already clears it when the company is switched.
//
// Problems are added to v; the returned error is only for unexpected (database)
// failures.
func (app *application) validateSupportRequests(v *validator.Validator, uses []data.SupportRequestUse) ([]data.SupportRequestRecord, error) {
	out := make([]data.SupportRequestRecord, 0, len(uses))

	shape := validator.New()
	data.ValidateSupportRequests(shape, uses)
	if !shape.Valid() {
		for key, msg := range shape.Errors {
			v.AddError(key, msg)
		}
		return nil, nil
	}
	if len(uses) == 0 {
		return out, nil
	}

	for _, u := range uses {
		if u.Status == "" {
			u.Status = data.SupportStatusPending
		}
		u.Notes = strings.TrimSpace(u.Notes)
		u.TelnetTechnician = trimmedOrNil(u.TelnetTechnician)
		u.TelnetConfirmedBy = trimmedOrNil(u.TelnetConfirmedBy)
		u.TelefonicaTicketID = trimmedOrNil(u.TelefonicaTicketID)

		switch u.Company {
		case data.SupportCompanyTelnet:
			// Only Telnet's own fields are meaningful here.
			u.TelefonicaTicketID = nil
		case data.SupportCompanyTelefonica:
			u.TelnetTechnician = nil
			u.TelnetConfirmedBy = nil
		}

		rec, ok := data.BuildSupportRequestRecord(u)
		if !ok {
			// The shape check above already rejected anything malformed, so
			// reaching here means the value is empty, not broken.
			v.AddError("support_requests", "contains an unreadable date and time")
			return nil, nil
		}
		// A solution dated before the report would be rejected by the database
		// as a 500, so it is caught here where it can be explained.
		if rec.StartedAt != nil && rec.ResolvedAt != nil && rec.ResolvedAt.Before(*rec.StartedAt) {
			v.AddError("support_requests", "the solution time cannot be before the reported time")
			return nil, nil
		}
		out = append(out, rec)
	}

	return out, nil
}

// trimmedOrNil normalizes an optional text field: surrounding whitespace is
// removed, and a field that is left empty becomes nil ("not provided") rather
// than an empty string that would fail the database's non-blank check.
func trimmedOrNil(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

// setIssueSupportRequests stores an issue's third-party handovers, writing the
// error response itself and reporting whether it did — the two failures it can
// produce (a repeated Telefónica ticket, or a handover that vanished while the
// form was open) are both things the user can fix by reopening the issue, so
// they come back as validation errors rather than a 500.
func (app *application) setIssueSupportRequests(w http.ResponseWriter, r *http.Request, issueID int64, uses []data.SupportRequestRecord, loggedBy int64) error {
	err := app.models.SupportRequests.SetForIssue(issueID, uses, loggedBy)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, data.ErrDuplicateSupportTicket):
		v := validator.New()
		v.AddError("support_requests", "the same Telefónica ticket id is already logged on this issue")
		app.failedValidationResponse(w, r, v.Errors)
	case errors.Is(err, data.ErrRecordNotFound):
		v := validator.New()
		v.AddError("support_requests", "one of the third-party handovers no longer exists on this issue — reopen the issue and try again")
		app.failedValidationResponse(w, r, v.Errors)
	default:
		app.serverErrorResponse(w, r, err)
	}
	return err
}

// listSupportRequestsHandler powers the third-party support page: every
// handover in an optional date range, plus the per-company turnaround summary
// for that same range.
//
// The range is filtered on created_at — when the handover was logged — which is
// the same anchor every other date filter in the app uses. An absent from or to
// is unbounded, so the page can open on "everything" and narrow from there.
//
// Read-only for every authenticated user, like /v1/consumables: handovers are
// edited through the issue they belong to, which keeps the owner-or-manager
// check in one place.
func (app *application) listSupportRequestsHandler(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	from := app.readString(qs, "from", "")
	to := app.readString(qs, "to", "")

	v := validator.New()
	for field, value := range map[string]string{"from": from, "to": to} {
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

	stats, err := app.models.SupportRequests.GetStats(from, to)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	requests, err := app.models.SupportRequests.GetList(from, to)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"requests": requests,
		"stats":    stats,
	}, nil)
}
