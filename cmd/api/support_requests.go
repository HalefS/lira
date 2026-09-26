package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// validateSupportRequests checks the third-party handovers sent with an issue
// create/update request and returns them normalized, ready to store.
//
// It runs in two steps, mirroring validateConsumableUses:
//  1. shape — company, status, and the fields each company actually needs
//     (data.ValidateSupportRequests);
//  2. normalization a browser form can't be trusted to have done: whitespace
//     trimmed, an empty text field treated as "not provided", and the fields
//     that don't belong to the chosen company cleared.
//
// That last part is deliberately forgiving rather than a validation error: a
// Telefonica ticket id left over on a Telnet handover is dropped, not rejected,
// because the database CHECK requires the field to be NULL there and the form
// already clears it when the company is switched.
//
// Problems are added to v; the returned error is only for unexpected (database)
// failures.
func (app *application) validateSupportRequests(v *validator.Validator, uses []data.SupportRequestUse) ([]data.SupportRequestUse, error) {
	out := make([]data.SupportRequestUse, len(uses))

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

	for i, u := range uses {
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
		out[i] = u
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
func (app *application) setIssueSupportRequests(w http.ResponseWriter, r *http.Request, issueID int64, uses []data.SupportRequestUse, loggedBy int64) error {
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
