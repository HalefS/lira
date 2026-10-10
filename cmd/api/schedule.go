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

// listTodayHandler is the dashboard's "who is on right now".
//
// PUBLIC, exactly as GET /v1/schedule, and for the same reason: it is a strict subset
// of what that endpoint already publishes -- the same member rows, today's cell instead
// of all seven. The `away` flag it adds publishes nothing the week view does not, since
// that view already carries the absences its week covers.
//
// It is deliberately a DIFFERENT path from /v1/schedule/... . The wildcard :user_id
// sits on a different method tree and would not actually collide -- verified against
// httprouter v1.3.0, GET /v1/schedule/today registers cleanly -- but the comments above
// /v1/schedule:user_id and /v1/rota/members are each about a DIFFERENT collision rule,
// and a third reader should not have to re-derive either of them to see why this one is
// named differently.
func (app *application) listTodayHandler(w http.ResponseWriter, r *http.Request) {
	today, err := app.models.Schedule.Today(time.Now())
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if today.Members == nil {
		today.Members = []*data.TodayMember{}
	}
	app.writeJSON(w, http.StatusOK, envelope{"today": today}, nil)
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

	// The one redaction on a public response in this feature. The note cannot be
	// here at all -- data.AbsenceRef has no field for it -- but the KIND can, and
	// "sick" beside a named colleague on a page with no login is publishing health
	// information about them. data.PublicKind reduces it to "unavailable" for
	// anyone who is not a manager, and it is a single function so this list and
	// listAbsencesHandler cannot drift on who is told what.
	//
	// can_edit is computed once and used for both the flag and the redaction. If
	// they were computed separately and one of them changed, the response could say
	// can_edit:true and still have redacted the kind -- a client that trusted the
	// flag would show "Unavailable" to a manager who had recorded "Sick leave".
	canEdit := app.canEditSchedule(r)
	for _, a := range week.Absences {
		a.Kind = data.PublicKind(a.Kind, canEdit)
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"week":     week.Week,
		"members":  week.Members,
		"shifts":   shifts,
		"absences": week.Absences,
		"kinds":    data.AbsenceKinds(),
		"can_edit": canEdit,

		// Four week-level facts, all additive, and all needed BY THE CLIENT because
		// none of them can be derived from what is already in the payload:
		//
		//   week_status    past | current | future. A boolean cannot say that Monday
		//                  is a record while Friday is still going to change next
		//                  week, and that is the difference the page has to draw.
		//   editable       false once the week has closed. Independent of can_edit:
		//                  a technician on an open week and a manager on a closed one
		//                  both see a read-only board, for different reasons.
		//   recorded       false when this week is rendering the standing pattern,
		//                  which is what lets the page say "this is inherited".
		//   history_exact  false for a past week rendered from the pattern because
		//                  it was never recorded. The page says so out loud rather
		//                  than presenting a guess as a record.
		"week_status":   week.Status,
		"editable":      week.Editable,
		"recorded":      week.Recorded,
		"history_exact": week.HistoryExact,
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
		// A plain string, not a pointer: a tint that is absent and a tint that is
		// empty are the same thing, so there is nothing for a pointer to
		// distinguish. See data.NormaliseShiftColor.
		Color string `json:"color"`
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
		Color:     input.Color,
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
		// A POINTER, for the same reason active is one below, and the consequence is
		// the whole reason. A plain string here would default to "" when the body
		// omitted it, and "" means "no tint" -- so every rename from the Settings
		// inline editor, and every retire, would silently wipe the colour somebody
		// had chosen. That client sends the whole row every time, which is correct
		// for a PUT-shaped endpoint and fatal for an omitted field here.
		Color   *string `json:"color"`
		Active  *bool   `json:"active"`
		Version int     `json:"version"`
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
		// Defaulted from the stored row so an editor that never mentions a tint
		// leaves it alone.
		Color:   existing.Color,
		Version: input.Version,
		// active is a pointer so "leave it as it is" is expressible. Defaulting it
		// to false would silently retire a shift whose editor never mentioned it,
		// and a retired shift disappears from every picker in the app.
		Active:    existing.Active,
		UpdatedBy: &existing.ID,
	}
	if input.Color != nil {
		shift.Color = *input.Color
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
	// The week being written is a QUERY PARAMETER, not the current week.
	//
	// That is the second half of 000035 and it is not optional: a PUT with no week
	// would have to mean "the week containing today", which makes every other week
	// unwritable and re-creates the original bug in a different shape -- the manager
	// looks at next Tuesday, edits it, and the write lands on this Tuesday instead.
	// Refusing an absent week is better than guessing one.
	anchor, ok := app.scheduleWeekAnchor(w, r)
	if !ok {
		return
	}
	if app.readString(r.URL.Query(), "week", "") == "" {
		app.errorResponse(w, r, http.StatusBadRequest,
			"week is required, as a date in YYYY-MM-DD format: a rota edit names the week it applies to")
		return
	}
	// The Monday, not the anchor. Every write below is keyed by week_start, and the
	// database has no notion of "the week containing this date" to normalise it.
	//
	// WeekOf rather than a new helper: it is the application's one definition of a
	// Monday-to-Sunday week, and a second one would be free to disagree with it.
	weekStart := data.WeekOf(anchor).Start.Time()

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
	if err := app.models.Schedule.SetWeekFor(userID, weekStart, input.Days, &editor.ID); err != nil {
		// 422, and a REASON the client switches on. The week being closed is a fact
		// about the world, not a malformed request and not a conflict with the
		// caller's own state, which is why it is neither 400 nor 409.
		//
		// The reason travels as a field rather than being inferred from the English
		// sentence. A message that changes when somebody rewords it is a message the
		// client cannot safely match on, and "week_closed" is the part a machine can
		// act on.
		if errors.Is(err, data.ErrWeekClosed) {
			app.writeJSON(w, http.StatusUnprocessableEntity, envelope{
				"message": "that week is closed and cannot be changed",
				"reason":  "week_closed",
				"week":    weekStart.Format("2006-01-02"),
			}, nil)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	// Answer with the member's row as it now stands, so the editor can confirm the
	// save without a second round trip and without trusting its own optimistic state.
	//
	// Echoed for THE WEEK THAT WAS WRITTEN, not for the current one. That looks like a
	// one-word detail and is not: saving next Tuesday's rota and being answered with
	// this week's row would leave the editor's grid showing last week's confirmation,
	// and the mismatch would read to the manager as the save having gone somewhere
	// unexpected.
	week, err := app.models.Schedule.Week(weekStart)
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
