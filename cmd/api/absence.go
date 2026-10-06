package main

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// Per-date absences: the dated exceptions to the recurring rota.
//
// The access split here is DELIBERATELY different from the rota's own, and the
// difference is the whole design. GET /v1/schedule is public with no requireAuth,
// because "who is on tonight" belongs on a noticeboard anybody can walk past. Every
// route in this file is manager-only, because what is behind them is a colleague's
// medical circumstances.
//
// So the boundary is drawn at the KIND as well as at the note:
//
//   - the note never travels in a public payload, and AbsenceRef -- the shape the
//     grid carries -- has no field for it at all, so it cannot leak without a type
//     change. A manager editing an absence fetches it from this file's endpoints.
//   - the kind is reduced for non-managers by data.PublicKind, which turns "sick"
//     into "unavailable". Publishing the word SICK beside a named colleague on a
//     page anybody can load is publishing health information about them, which is a
//     different and larger thing than publishing that they are off.
//
// The trade is deliberate and lives in one place: data.PublicKind. A manager sees
// the real kind, so somebody asking why a colleague is away gets an answer from a
// manager rather than from the board.

// absenceInput is the body of a create or an update.
//
// Dates are strings, not data.JSONDate, and that is not laziness. JSONDate is
// marshal-only -- MarshalJSON and no UnmarshalJSON -- so encoding/json cannot bind
// "2026-11-02" into it and the field would silently stay the zero time. This is
// the house pattern for a date in a body, and maintenance.go's done_on does
// exactly this: a string, dateParamPattern to check the shape, time.Parse to
// produce the value.
//
// Adding an UnmarshalJSON to JSONDate would be the tidier fix and the wrong one
// here: it would change how every other JSONDate-bearing request body parses across
// the whole API, silently, in a change whose subject is a holiday table.
//
// user_id is absent from the update path on purpose -- see updateAbsenceHandler.
type absenceInput struct {
	UserID   int64  `json:"user_id"`
	Kind     string `json:"kind"`
	StartsOn string `json:"starts_on"`
	EndsOn   string `json:"ends_on"`
	Reason   string `json:"reason"`
	Version  int    `json:"version"`
}

// absenceToAbsence parses the two dates onto a and adds a field error for each bad
// one, reporting whether the caller may carry on.
//
// It adds to the SAME validator the rest of the absence is checked with, rather
// than writing a response of its own, so a body with a bad kind AND a bad date
// comes back as both rather than as whichever was checked first. That is why it
// takes the validator instead of the ResponseWriter.
//
// ends_on is INCLUSIVE, so a one-day absence is the same date twice. Not a special
// case anywhere: a manager typing Monday to Wednesday means three days, and a
// manager typing Monday to Monday means one.
func (app *application) absenceToAbsence(v *validator.Validator, in *absenceInput, a *data.Absence) bool {
	ok := true

	if !dateParamPattern.MatchString(in.StartsOn) {
		v.AddError("starts_on", "must be a date in YYYY-MM-DD format")
		ok = false
	} else if d, err := time.Parse(time.DateOnly, in.StartsOn); err == nil {
		a.StartsOn = data.JSONDate(d)
	} else {
		ok = false
	}

	if !dateParamPattern.MatchString(in.EndsOn) {
		v.AddError("ends_on", "must be a date in YYYY-MM-DD format")
		ok = false
	} else if d, err := time.Parse(time.DateOnly, in.EndsOn); err == nil {
		a.EndsOn = data.JSONDate(d)
	} else {
		ok = false
	}

	return ok
}

// listAbsencesHandler returns absences over a window, for one person or for all.
//
// MANAGER ONLY, unlike the rota grid next door, because this response carries the
// note. The window is read from from/to and defaults to a year either side of
// today, so a manager who opens it with no dates sees everything that could
// possibly be on a board today rather than a window they did not choose.
func (app *application) listAbsencesHandler(w http.ResponseWriter, r *http.Request) {
	from, to := app.absenceWindow(r)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var (
		absences []*data.Absence
		err      error
	)
	if raw := app.readString(r.URL.Query(), "user_id", ""); raw != "" {
		userID, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil || userID < 1 {
			app.notFoundResponse(w, r)
			return
		}
		absences, err = app.models.Absences.ListForUser(userID, from, to)
	} else {
		absences, err = app.models.Absences.InWindow(ctx, from, to)
	}
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if absences == nil {
		absences = []*data.Absence{}
	}

	// The kinds are reduced here too, for a technician who is not a manager. This
	// endpoint is manager-gated so the note is safe either way, but canEditSchedule
	// is the one definition of "is this caller a manager" in this package, and
	// reusing it is what stops this list and the grid drifting on who sees what.
	for _, a := range absences {
		a.Kind = data.PublicKind(a.Kind, app.canEditSchedule(r))
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"absences": absences,
		"can_edit": app.canEditSchedule(r),
		"kinds":    data.AbsenceKinds(),
		"from":     from.Format(time.DateOnly),
		"to":       to.Format(time.DateOnly),
	}, nil)
}

// createAbsenceHandler records somebody being away for a span of dates.
//
// Absence writes are immediate rather than staged through the rota's Save button.
// That is deliberate: they are not rota cells, they have their own endpoints, and
// folding them into a draft that a later Cancel could throw away would make a
// holiday something a manager could lose by changing their mind about a shift.
func (app *application) createAbsenceHandler(w http.ResponseWriter, r *http.Request) {
	var in absenceInput
	if err := app.readJSON(w, r, &in); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	a := &data.Absence{
		UserID:    in.UserID,
		Kind:      in.Kind,
		Reason:    in.Reason,
		CreatedBy: &user.ID,
	}

	// Validation BEFORE existence, which is the reverse of setScheduleHandler's order
	// and deliberate. `user_id: 0` is a malformed field and deserves a 422 that names
	// it; a well-formed id for somebody who is not here deserves a 404. Checking
	// existence first answers both with a 404, and -- the part that actually matters
	// -- it leaves ValidateAbsence's user_id rule unreachable over HTTP. A rule no
	// request can exercise is a rule nobody maintains.
	v := validator.New()
	if app.absenceToAbsence(v, &in, a) {
		data.ValidateAbsence(v, a)
	}
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	// The person has to exist, at the door rather than as a constraint violation
	// halfway through the INSERT.
	if _, err := app.models.Users.Get(a.UserID); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	// No overlap check, on purpose. Holiday and sick leave genuinely collide and both
	// facts are true, so there is no invariant to enforce and therefore no
	// check-then-insert to make race-safe. The trap being avoided is the one
	// rooms.go records in full: SELECT ... FOR UPDATE locks nothing when it matched
	// nothing, so two managers checking simultaneously both see a free slot and both
	// insert. Migration 000031 says the same at greater length.
	if err := app.models.Absences.Create(a); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// The stored row rather than the submitted one, so a client that sent "Sick" or
	// a padded reason sees what was actually kept.
	app.writeJSON(w, http.StatusCreated, envelope{
		"absence":   a,
		"day_count": a.DayCount(),
	}, nil)
}

// updateAbsenceHandler edits an absence, guarded by the version it was read at.
//
// An absence genuinely IS a read-modify-write -- extending a holiday is an UPDATE
// of one row -- so unlike a rota cell it can be lost to a concurrent edit. The loser
// gets 409, not 404: the record is still there, somebody else saved first, and
// saying it is gone would send the manager looking for something that is not
// missing.
func (app *application) updateAbsenceHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	existing, err := app.models.Absences.Get(id)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	var in absenceInput
	if err := app.readJSON(w, r, &in); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	// The person is NOT editable, and the stored row wins. An absence belongs to a
	// person; moving one to another is a delete and a create, not an edit, and the
	// note travels with the record -- re-pointing the row would hand one colleague's
	// medical detail to another. A body that names a different user_id is therefore
	// quietly ignored rather than honoured, which is why this is not a 422: the
	// field is not part of the edit.
	a := &data.Absence{
		ID:        id,
		UserID:    existing.UserID,
		Kind:      in.Kind,
		Reason:    in.Reason,
		StartsOn:  existing.StartsOn,
		EndsOn:    existing.EndsOn,
		Version:   in.Version,
		UpdatedBy: &app.contextGetUser(r).ID,
	}

	v := validator.New()
	if app.absenceToAbsence(v, &in, a) {
		data.ValidateAbsence(v, a)
	}
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.Absences.Update(a); err != nil {
		if errors.Is(err, data.ErrEditConflict) {
			app.errorResponse(w, r, http.StatusConflict,
				"this absence was changed by somebody else while you had it open; reload and try again")
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"absence":   a,
		"day_count": a.DayCount(),
	}, nil)
}

// deleteAbsenceHandler removes an absence.
//
// Deliberately does NOT touch the rota cells the person held on those dates. An
// absence and an assignment are independent facts, and clearing somebody's week
// because they were off sick for three days would quietly destroy the standing
// pattern behind it. The response says so, because "the days are removed" is the
// assumption a manager is most likely to arrive with.
func (app *application) deleteAbsenceHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	existing, err := app.models.Absences.Get(id)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.models.Absences.Delete(id); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"message":           "absence successfully deleted",
		"day_count":         existing.DayCount(),
		"assignments_kept":  true,
		"schedule_restored": "the recurring rota for those dates is unchanged",
	}, nil)
}

// absenceWindow reads from/to for the list, defaulting to a year either side of
// today.
//
// Reversed bounds are swapped rather than refused: a manager who typed them the
// wrong way round gets the sensible answer rather than an empty list, and an empty
// list here reads as "this person has never been away" rather than as a mistake in
// the request.
func (app *application) absenceWindow(r *http.Request) (time.Time, time.Time) {
	today := data.Today()
	from := today.AddDate(0, -1, 0)
	to := today.AddDate(1, 0, 0)

	if raw := app.readString(r.URL.Query(), "from", ""); raw != "" {
		if d, err := time.Parse(time.DateOnly, raw); err == nil {
			from = d
		}
	}
	if raw := app.readString(r.URL.Query(), "to", ""); raw != "" {
		if d, err := time.Parse(time.DateOnly, raw); err == nil {
			to = d
		}
	}
	if to.Before(from) {
		from, to = to, from
	}
	return from, to
}
