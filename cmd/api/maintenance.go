package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// Preventive maintenance on department equipment.
//
// The access split is deliberate and not uniform: anybody signed in can record
// that a check was carried out, because the people doing the work are the
// technicians, not the managers. Only a manager can add, edit or delete a
// device, because that is configuration and a technician who could edit the
// schedule could edit their own backlog out of existence.

// maintenanceToday is the server's idea of today, used for due-date maths so a
// technician whose laptop clock is wrong still sees the queue the server thinks
// is real. Matches how every other date filter in the app is anchored.
func maintenanceToday() time.Time {
	t := data.Today()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// maintenanceVisibleTo reports whether a user may see or act on one device.
//
// A manager always can: they administer the schedule. A technician can when the
// device is theirs or is nobody's in particular.
//
// Checked on every endpoint that takes a device id, not just on the list. Without
// it the scoping is only cosmetic -- a technician who guessed or was told an id
// could read another person's history, or record a check against their printer,
// through the same API the page uses.
func maintenanceVisibleTo(user *data.User, s *data.MaintenanceSchedule) bool {
	if user.Role == "manager" {
		return true
	}
	if s.AssigneeID == nil {
		return true
	}
	return *s.AssigneeID == user.ID
}

// maintenanceScheduleFor resolves a device id to a device this user may reach.
//
// A device that exists but is somebody else's answers 404 rather than 403, the
// same as one that does not exist. Saying "forbidden" would confirm the id is
// real, which is a small thing to give away and tells the technician nothing they
// can act on.
func (app *application) maintenanceScheduleFor(w http.ResponseWriter, r *http.Request) (*data.MaintenanceSchedule, bool) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return nil, false
	}
	s, err := app.models.Maintenance.GetSchedule(id)
	if err != nil {
		app.notFoundResponse(w, r)
		return nil, false
	}
	if !maintenanceVisibleTo(app.contextGetUser(r), s) {
		app.notFoundResponse(w, r)
		return nil, false
	}
	return s, true
}

// maintenanceRespond writes a schedule back after a create or an edit.
//
// The handler only has the assignee's id -- the name is joined in by the read
// queries -- so echoing the struct that was just submitted would answer with a
// blank assignee even when one was set. Re-read rather than hand the name over
// separately, so the answer is exactly what a later GET would return.
func (app *application) maintenanceRespond(w http.ResponseWriter, r *http.Request, s *data.MaintenanceSchedule, status int) {
	saved, err := app.models.Maintenance.GetSchedule(s.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	today := maintenanceToday()
	saved.DueState = data.MaintenanceDueState(saved.NextDueOn.Time(), today, saved.Active)
	app.writeJSON(w, status, envelope{"schedule": saved}, nil)
}

func (app *application) listMaintenanceSchedulesHandler(w http.ResponseWriter, r *http.Request) {
	today := maintenanceToday()
	user := app.contextGetUser(r)
	isManager := user.Role == "manager"

	// Scoped in the query, not filtered afterwards: the checks that are not this
	// technician's should never leave the database at all. A manager is the
	// exception -- they administer the schedule rather than work through it, so
	// they always see everything.
	schedules, err := app.models.Maintenance.ListSchedules(today, user.ID, isManager)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if schedules == nil {
		schedules = []*data.MaintenanceSchedule{}
	}

	// The badge count, over every active device the caller may see rather than
	// over the list returned here, so it cannot disagree with itself.
	dueCount, err := app.models.Maintenance.CountDue(today, user.ID, isManager)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	recent, err := app.models.Maintenance.ListAllChecks(app.readInt(r.URL.Query(), "recent", 10), user.ID, isManager)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if recent == nil {
		recent = []*data.MaintenanceCheck{}
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"schedules": schedules,
		"recent":    recent,
		"due_count": dueCount,
		"today":     today.Format(time.DateOnly),
	}, nil)
}

func (app *application) createMaintenanceScheduleHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DeviceName    string `json:"device_name"`
		EquipmentType string `json:"equipment_type"`
		DepartmentID  int64  `json:"department_id"`
		IntervalDays  int    `json:"interval_days"`
		// Null means everybody, which is the default for a check nobody has been
		// given to yet.
		AssigneeID *int64 `json:"assignee_id"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	// An interval left out takes the class default rather than being rejected:
	// "add the printer in Reception" is the normal request and the manager
	// should not have to know what we think a printer is.
	if input.IntervalDays == 0 {
		input.IntervalDays = data.DefaultIntervalDays(input.EquipmentType)
	}

	user := app.contextGetUser(r)
	createdBy := user.ID
	s := &data.MaintenanceSchedule{
		DeviceName:    strings.TrimSpace(input.DeviceName),
		EquipmentType: input.EquipmentType,
		DepartmentID:  input.DepartmentID,
		IntervalDays:  input.IntervalDays,
		Active:        true,
		AssigneeID:    input.AssigneeID,
		CreatedBy:     &createdBy,
	}

	v := validator.New()
	data.ValidateMaintenanceSchedule(v, s)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	// The department has to exist. Checked here rather than left to the foreign
	// key so a mistyped id comes back as a validation failure the UI can put
	// next to the field, instead of a 500 from the insert.
	if _, err := app.models.Departments.Get(input.DepartmentID); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			v.AddError("department_id", "does not exist")
			app.failedValidationResponse(w, r, v.Errors)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	// The assignee has to be a real account, checked here rather than left to the
	// foreign key so a stale id comes back as a validation failure the UI can put
	// next to the field, instead of a 500. A deactivated account is fine -- the
	// check is still assigned, it just has nobody pressing buttons for it, which
	// is the manager's business to reassign.
	if input.AssigneeID != nil {
		if _, err := app.models.Users.Get(*input.AssigneeID); err != nil {
			if errors.Is(err, data.ErrRecordNotFound) {
				v.AddError("assignee_id", "does not exist")
				app.failedValidationResponse(w, r, v.Errors)
				return
			}
			app.serverErrorResponse(w, r, err)
			return
		}
	}
	if err := app.models.Maintenance.InsertSchedule(s); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.maintenanceRespond(w, r, s, http.StatusCreated)
}

func (app *application) updateMaintenanceScheduleHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	var input struct {
		DeviceName    string `json:"device_name"`
		EquipmentType string `json:"equipment_type"`
		DepartmentID  int64  `json:"department_id"`
		IntervalDays  int    `json:"interval_days"`
		Active        *bool  `json:"active"`
		// Null means the check belongs to everybody, which is a real value rather
		// than an absent one -- so this is a pointer and not a plain id.
		//
		// This endpoint replaces the record rather than patching it: every field
		// is written, so omitting assignee_id hands the check back to the pool.
		// That is deliberate and matches the form, which always sends the whole
		// thing; what would not be safe is a partial PATCH, and this is not one.
		AssigneeID *int64 `json:"assignee_id"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	existing, err := app.models.Maintenance.GetSchedule(id)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	s := &data.MaintenanceSchedule{
		ID:            id,
		DeviceName:    strings.TrimSpace(input.DeviceName),
		EquipmentType: input.EquipmentType,
		DepartmentID:  input.DepartmentID,
		IntervalDays:  input.IntervalDays,
		// A nil means "the request did not say", which must not silently
		// retire the device. Absent field = leave it as it was.
		Active:     existing.Active,
		Version:    existing.Version,
		AssigneeID: input.AssigneeID,
		CreatedBy:  existing.CreatedBy,
	}
	if input.Active != nil {
		s.Active = *input.Active
	}
	user := app.contextGetUser(r)
	updatedBy := user.ID
	s.UpdatedBy = &updatedBy

	v := validator.New()
	data.ValidateMaintenanceSchedule(v, s)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if _, err := app.models.Departments.Get(input.DepartmentID); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			v.AddError("department_id", "does not exist")
			app.failedValidationResponse(w, r, v.Errors)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	// The assignee has to be a real account, checked here rather than left to the
	// foreign key so a stale id comes back as a validation failure the UI can put
	// next to the field, instead of a 500. A deactivated account is fine -- the
	// check is still assigned, it just has nobody pressing buttons for it, which
	// is the manager's business to reassign.
	if input.AssigneeID != nil {
		if _, err := app.models.Users.Get(*input.AssigneeID); err != nil {
			if errors.Is(err, data.ErrRecordNotFound) {
				v.AddError("assignee_id", "does not exist")
				app.failedValidationResponse(w, r, v.Errors)
				return
			}
			app.serverErrorResponse(w, r, err)
			return
		}
	}
	if err := app.models.Maintenance.UpdateSchedule(s); err != nil {
		// A version clash is a 409, not a 404: the device is still there,
		// someone else simply saved it first.
		if errors.Is(err, data.ErrRecordNotFound) {
			app.writeJSON(w, http.StatusConflict,
				envelope{"error": "this device was changed by someone else — reload and try again"}, nil)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	app.maintenanceRespond(w, r, s, http.StatusOK)
}

func (app *application) deleteMaintenanceScheduleHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}
	if err := app.models.Maintenance.DeleteSchedule(id); err != nil {
		app.notFoundResponse(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// recordMaintenanceCheckHandler files a completed check.
//
// Open to any signed-in user. This is the technician saying "I have looked at
// this", and it is the whole point of the feature being useful: if only managers
// could do it, the technicians doing the work would have nothing to record it in.
func (app *application) recordMaintenanceCheckHandler(w http.ResponseWriter, r *http.Request) {
	// Scope before reading the body, so a request against somebody else's printer
	// is refused on the same terms as one against an id that does not exist.
	schedule, ok := app.maintenanceScheduleFor(w, r)
	if !ok {
		return
	}

	var input struct {
		Outcome string `json:"outcome"`
		Note    string `json:"note"`
		// Optional, and only meaningful alongside a fault. A clean check has
		// nothing to point at, and the note is where a fault that was put right
		// on the spot is described.
		IssueID *int64 `json:"issue_id"`
		DoneOn  string `json:"done_on"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	check := &data.MaintenanceCheck{
		ScheduleID: schedule.ID,
		Outcome:    input.Outcome,
		Note:       input.Note,
		DoneBy:     user.ID,
		DoneOn:     data.JSONDate(maintenanceToday()),
		IssueID:    input.IssueID,
	}

	v := validator.New()
	data.ValidateMaintenanceCheck(v, check)

	if input.DoneOn != "" {
		if !dateParamPattern.MatchString(input.DoneOn) {
			v.AddError("done_on", "must be a date in YYYY-MM-DD format")
		} else if d, perr := time.Parse(time.DateOnly, input.DoneOn); perr == nil {
			check.DoneOn = data.JSONDate(d)
		}
	}
	// A check cannot be filed for a day that has not happened, and one filed far
	// in the past would silently push the next due date into the past with it.
	if check.DoneOn.Time().After(maintenanceToday()) {
		v.AddError("done_on", "must not be in the future")
	}

	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	// A linked fault has to point at an issue that exists. Done as a check
	// rather than left to the foreign key so a stale id is a clear message.
	if check.IssueID != nil {
		if _, err := app.models.Issues.Get(*check.IssueID); err != nil {
			if errors.Is(err, data.ErrRecordNotFound) {
				v.AddError("issue_id", "does not exist")
				app.failedValidationResponse(w, r, v.Errors)
				return
			}
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	if _, err := app.models.Maintenance.RecordCheck(check); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusCreated, envelope{"check": check}, nil)
}

// linkMaintenanceCheckHandler attaches an issue to a check already recorded.
//
// Separate from the create call because the two happen in the opposite order to
// what the API surface suggests: the technician records the check first, then
// decides it is worth logging, then writes the issue in the normal way. This
// closes that last gap without making the check wait on an issue that does not
// exist yet.
func (app *application) linkMaintenanceCheckHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	// A check is addressed by its own id here, so the device is resolved from it
	// rather than assumed. Linking somebody else's check would otherwise be a way
	// to attach an issue to their history.
	scheduleID, err := app.models.Maintenance.CheckScheduleID(id)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}
	schedule, err := app.models.Maintenance.GetSchedule(scheduleID)
	if err != nil || !maintenanceVisibleTo(app.contextGetUser(r), schedule) {
		app.notFoundResponse(w, r)
		return
	}

	var input struct {
		IssueID *int64 `json:"issue_id"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	if input.IssueID != nil {
		if _, err := app.models.Issues.Get(*input.IssueID); err != nil {
			if errors.Is(err, data.ErrRecordNotFound) {
				v := validator.New()
				v.AddError("issue_id", "does not exist")
				app.failedValidationResponse(w, r, v.Errors)
				return
			}
			app.serverErrorResponse(w, r, err)
			return
		}
	}

	if err := app.models.Maintenance.LinkIssue(id, input.IssueID); err != nil {
		app.notFoundResponse(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (app *application) listMaintenanceChecksHandler(w http.ResponseWriter, r *http.Request) {
	// Resolved through the visibility check rather than a bare existence check:
	// otherwise a technician could read another person's history by guessing an
	// id, which is the one thing the assignment is meant to prevent.
	s, ok := app.maintenanceScheduleFor(w, r)
	if !ok {
		return
	}

	checks, err := app.models.Maintenance.ListChecks(s.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if checks == nil {
		checks = []*data.MaintenanceCheck{}
	}
	app.writeJSON(w, http.StatusOK, envelope{"checks": checks}, nil)
}
