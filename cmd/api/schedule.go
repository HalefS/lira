package main

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
	"github.com/julienschmidt/httprouter"
)

// The weekly team rota.
//
// Two access levels on the read side, and the split is the point of the feature: a
// noticeboard on the wall outside the office has to be readable by anybody walking
// past it, and the shift pattern is exactly that -- who is on tonight is not secret.
// Every write is manager-only, the same gate every other catalog in this API uses.
//
// The read handlers are deliberately registered WITHOUT requireAuth. authenticate
// has already resolved the caller by the time any handler runs and puts
// data.AnonymousUser in the request context when there was no token, so leaving
// requireAuth off is what makes a route public; there is no "optional auth"
// middleware in this codebase and inventing one for two routes would be a worse
// trade than reading the context directly.

// scheduleWeekAnchor reads and validates the ?week= anchor on the read endpoints. An
// absent anchor means the current week.
//
// A date anywhere inside the wanted week is accepted, not just its Monday, because
// that is what a person clicking "next week" means: they are holding a date they
// can see on the calendar and asking for the week it falls in. Requiring a Monday
// would make the caller do the arithmetic, and would make the API's contract differ
// from the page's.
//
// It writes the error response itself and reports whether the caller may carry on,
// so both handlers stay a single call at the top.
func (app *application) scheduleWeekAnchor(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	raw := app.readString(r.URL.Query(), "week", "")
	if raw == "" {
		return time.Now(), true
	}
	anchor, err := time.ParseInLocation("2006-01-02", raw, time.Local)
	if err != nil {
		app.errorResponse(w, r, http.StatusBadRequest, "week must be a date in YYYY-MM-DD format")
		return time.Time{}, false
	}
	return anchor, true
}

// canEditSchedule reports whether this caller may change the rota.
//
// Three answers, not two, and the middle one is why this is a function rather than a
// role check at each handler: an anonymous viewer, an authenticated technician and a
// manager each need to be told something different by the frontend, and the frontend
// cannot work it out on its own. It knows who it has a token for, but a token is not
// a role, and hardcoding the rule in the client means a second place to get wrong
// when roles change.
//
// It is advisory. The server enforces the same rule with requireManager on every
// write, so a client that ignores this and offers an Edit button anyway gets a 403
// rather than a corrupted rota.
func (app *application) canEditSchedule(r *http.Request) bool {
	user := app.contextGetUser(r)
	return !user.IsAnonymous() && user.Role == "manager"
}

// listScheduleHandler returns the rota for a week.
//
// PUBLIC. No requireAuth wrapper, for the reason at the top of this file.
//
// The response carries can_edit rather than making the client infer it. The grid
// needs it to decide between two genuinely different renderings -- a read-only
// noticeboard and an editable one -- and the answer depends on the caller's role,
// which the browser does not have.
func (app *application) listScheduleHandler(w http.ResponseWriter, r *http.Request) {
	anchor, ok := app.scheduleWeekAnchor(w, r)
	if !ok {
		return
	}

	week, err := app.models.Schedule.Week(anchor)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// Shifts travel with the grid rather than in a second call. A cell already
	// carries its shift, so the extra request would only be to build the legend and
	// the picker -- and on the public page there is no picker at all, so for an
	// anonymous viewer it would be a request made for nothing.
	shifts, err := app.models.Shifts.List(false)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if shifts == nil {
		shifts = []*data.Shift{}
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"week":     week.Week,
		"members":  week.Members,
		"shifts":   shifts,
		"can_edit": app.canEditSchedule(r),
	}, nil)
}

// listShiftsHandler returns the shift templates a cell can be set to.
//
// PUBLIC, and deliberately a narrower list than Settings shows: only the active
// ones. A retired shift is history, and offering it in a picker would put a cell on
// screen that nobody intends anybody to be on.
//
// include_retired lets a manager -- and only a manager -- ask for the full list, so
// the Settings catalog can be one endpoint rather than two. A request for retired
// shifts from anybody else is simply ignored rather than refused: they would have
// been refused the edit anyway, and the response they get is the same one a
// non-manager gets by default.
func (app *application) listShiftsHandler(w http.ResponseWriter, r *http.Request) {
	includeRetired := app.canEditSchedule(r) && app.readString(r.URL.Query(), "include_retired", "") == "true"

	shifts, err := app.models.Shifts.List(includeRetired)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if shifts == nil {
		shifts = []*data.Shift{}
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"shifts":          shifts,
		"can_edit":        app.canEditSchedule(r),
		"max_name_length": data.MaxShiftNameLength,
	}, nil)
}

// createShiftHandler adds a shift template.
func (app *application) createShiftHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name      string `json:"name"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	shift := &data.Shift{
		Name:      input.Name,
		StartTime: input.StartTime,
		EndTime:   input.EndTime,
		// Active defaults to true here rather than being read from the body, so a
		// client cannot create a shift that is already retired and invisible to
		// everybody else. Retiring is a separate, deliberate PATCH.
		Active:    true,
		CreatedBy: &user.ID,
	}

	v := validator.New()
	data.ValidateShift(v, shift)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.Shifts.Create(shift); err != nil {
		if errors.Is(err, data.ErrDuplicateShift) {
			// A field error rather than a bare conflict, because the name is a form
			// field with a value in it and the message belongs next to the input --
			// the same shape createDepartmentHandler uses for a duplicate name.
			v := validator.New()
			v.AddError("name", "a shift with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"shift": shift}, nil)
}

// updateShiftHandler edits a shift template, guarded by the version it was read at.
//
// A version that no longer matches comes back as 409 with a message saying so,
// rather than as a 404: the shift is still there, somebody else simply saved first,
// and telling the manager it has gone would send them looking for something that is
// not missing.
func (app *application) updateShiftHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	var input struct {
		Name      string `json:"name"`
		StartTime string `json:"start_time"`
		EndTime   string `json:"end_time"`
		Active    *bool  `json:"active"`
		Version   int    `json:"version"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	existing, err := app.models.Shifts.Get(id)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	shift := &data.Shift{
		ID:        id,
		Name:      input.Name,
		StartTime: input.StartTime,
		EndTime:   input.EndTime,
		Version:   input.Version,
		// active is a pointer so "leave it as it is" is expressible. Defaulting it
		// to false would silently retire a shift whose editor never mentioned it,
		// and a retired shift disappears from every picker in the app.
		Active:    existing.Active,
		UpdatedBy: &existing.ID,
	}
	if input.Active != nil {
		shift.Active = *input.Active
	}
	shift.UpdatedBy = &app.contextGetUser(r).ID

	v := validator.New()
	data.ValidateShift(v, shift)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if err := app.models.Shifts.Update(shift); err != nil {
		switch {
		case errors.Is(err, data.ErrEditConflict):
			app.errorResponse(w, r, http.StatusConflict,
				"this shift was changed by somebody else while you had it open; reload and try again")
		case errors.Is(err, data.ErrDuplicateShift):
			v := validator.New()
			v.AddError("name", "a shift with this name already exists")
			app.failedValidationResponse(w, r, v.Errors)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"shift": shift}, nil)
}

// deleteShiftHandler removes a shift, and by cascade the cells that pointed at it.
//
// The count of affected cells is in the response so a client that wants to warn
// first can ask before pressing the button rather than apologising afterwards.
func (app *application) deleteShiftHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	shift, err := app.models.Shifts.Get(id)
	if err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	if err := app.models.Shifts.Delete(id); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"message":             "shift successfully deleted",
		"cleared_assignments": shift.AssignmentCount,
	}, nil)
}

// setScheduleHandler writes one member's week.
//
// A PUT carrying all seven cells rather than a PATCH per cell, so a manager who
// changes three days in a row save sees one write and cannot end up with half a week
// on screen and half in the database. Days the request leaves out are cleared, which
// is what makes "put them on days off" a single request rather than seven.
func (app *application) setScheduleHandler(w http.ResponseWriter, r *http.Request) {
	// Read by name rather than through readIDParam, which is hardcoded to the
	// param "id". This route's segment is "user_id" on purpose -- the handler acts
	// on a person rather than on a shift, and conflating the two in the path is
	// how a misrouted request ends up editing the wrong record.
	userID, err := strconv.ParseInt(httprouter.ParamsFromContext(r.Context()).ByName("user_id"), 10, 64)
	if err != nil || userID < 1 {
		app.notFoundResponse(w, r)
		return
	}

	// A manager may only edit the rota for somebody who exists. The FK on the
	// assignment would catch this anyway, but as a constraint violation halfway
	// through a transaction rather than as a 404 at the door.
	if _, err := app.models.Users.Get(userID); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	var input struct {
		Days []data.DayAssignment `json:"days"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	data.ValidateDayAssignments(v, input.Days)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	// Every shift referenced must exist. Checked up front so a request naming three
	// good days and one bad shift changes nothing at all, rather than writing the
	// three and rolling back on the fourth -- which it would, but the manager would
	// be told about the fourth rather than about the fact that nothing saved.
	for _, d := range input.Days {
		if d.ShiftID == nil {
			continue
		}
		if _, err := app.models.Shifts.Get(*d.ShiftID); err != nil {
			if errors.Is(err, data.ErrRecordNotFound) {
				v := validator.New()
				v.AddError("days", "one of the shifts named does not exist")
				app.failedValidationResponse(w, r, v.Errors)
				return
			}
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	editor := app.contextGetUser(r)
	if err := app.models.Schedule.SetWeek(userID, input.Days, &editor.ID); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// Answer with the member's row as it now stands, so the editor can confirm the
	// save without a second round trip and without trusting its own optimistic state.
	week, err := app.models.Schedule.Week(time.Now())
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	for _, m := range week.Members {
		if m.UserID == userID {
			app.writeJSON(w, http.StatusOK, envelope{"member": m}, nil)
			return
		}
	}
	app.writeJSON(w, http.StatusOK, envelope{"message": "schedule successfully updated"}, nil)
}
