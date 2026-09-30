package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// maxLCUNoteLength bounds the free-text note on a day's result and on the unit.
const maxLCUNoteLength = 200

// maxLCUResultsPerRequest bounds one submission, so a stray client cannot make
// the handler loop over an unbounded list.
const maxLCUResultsPerRequest = 50

// lcuTestRequiredCode is the marker the frontend looks for to tell "you must
// answer today's LCU test first" apart from an ordinary permission failure. It
// travels in the body rather than the status line, because both cases are 403.
const lcuTestRequiredCode = "lcu_test_required"

// lcuGateAllows lists the paths that stay reachable while a user has an
// unanswered test. Everything else is refused.
//
// The frontend asks /v1/lcu/today first and shows the prompt from the answer, so
// these two endpoints are what the prompt itself needs: one to learn what is
// outstanding, one to record it. The health check is left open so a monitor
// polling the server is not locked out by somebody's test.
func lcuGateAllows(path string) bool {
	switch path {
	case "/v1/lcu/today", "/v1/lcu/tests", "/v1/healthcheck":
		return true
	}
	return false
}

// lcuDecisionRequiredResponse tells the caller their day's result is outstanding
// and hands back the units it applies to, so the prompt can be rendered from the
// same response that refused the work.
func (app *application) lcuDecisionRequiredResponse(w http.ResponseWriter, r *http.Request, units []*data.LCUUnit) {
	env := envelope{
		"error": "record today's LCU test result before continuing",
		"code":  lcuTestRequiredCode,
		"today": data.Today().Format("2006-01-02"),
		"units": units,
	}
	if err := app.writeJSON(w, http.StatusForbidden, env, nil); err != nil {
		app.logError(r, err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// lcuGate refuses work from a user who still owes a result for today.
//
// This is the server half of "if you do not provide a response you cannot use
// the application". The blocking dialog on its own would only be a suggestion:
// a refresh, a second tab, or anyone poking at the API directly would walk
// straight past it. Checking here means the answer is genuinely required before
// anything else happens.
//
// The check is one indexed EXISTS against a table that only holds the readers
// currently on trial, and the request already costs a token lookup in
// authenticate, so it does not change the shape of the request path.
func (app *application) lcuGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := app.contextGetUser(r)
		if user.IsAnonymous() || lcuGateAllows(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		pending, err := app.models.LCU.PendingToday(user.ID, data.Today())
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
		if len(pending) > 0 {
			app.lcuDecisionRequiredResponse(w, r, pending)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// canAnswerFor reports whether this user is allowed to record results for a
// unit: the technician who put it on trial, or any manager. Managers can cover
// for someone who is off, and can also answer for another technician's reader.
func canAnswerFor(user *data.User, unit *data.LCUUnit) bool {
	if user.Role == "manager" {
		return true
	}
	return unit.AddedBy != nil && *unit.AddedBy == user.ID
}

// lcuTodayHandler answers the one question the frontend asks on load: is there a
// result owed for today, and if so for which readers.
func (app *application) lcuTodayHandler(w http.ResponseWriter, r *http.Request) {
	today := data.Today()

	// Close out any window that has ended, so a reader that finished its week
	// while nobody was looking is not still shown as on trial.
	if _, err := app.models.LCU.ResolveDue(today); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	pending, err := app.models.LCU.PendingToday(user.ID, today)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"required": len(pending) > 0,
		"today":    today.Format("2006-01-02"),
		"units":    pending,
	}, nil)
}

// listLCUUnitsHandler returns the readers on trial. A manager sees everyone's;
// everyone else sees only their own.
func (app *application) listLCUUnitsHandler(w http.ResponseWriter, r *http.Request) {
	today := data.Today()
	if _, err := app.models.LCU.ResolveDue(today); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	user := app.contextGetUser(r)
	var (
		units []*data.LCUUnit
		err   error
	)
	if user.Role == "manager" {
		units, err = app.models.LCU.ListAll()
	} else {
		units, err = app.models.LCU.ListForUser(user.ID)
	}
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if units == nil {
		units = []*data.LCUUnit{}
	}

	// can_answer lets the UI show the answer control only where it will work.
	// The server checks the same rule on write, so this is convenience, not the
	// enforcement.
	type withPermission struct {
		*data.LCUUnit
		CanAnswer bool `json:"can_answer"`
	}
	out := make([]withPermission, 0, len(units))
	for _, u := range units {
		out = append(out, withPermission{LCUUnit: u, CanAnswer: canAnswerFor(user, u)})
	}

	app.writeJSON(w, http.StatusOK, envelope{"units": out}, nil)
}

// createLCUUnitHandler puts a reader on trial. The seven-day window opens today,
// so a reader added this morning has to be tested this morning.
func (app *application) createLCUUnitHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Serial string `json:"serial"`
		Note   string `json:"note"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	data.ValidateLCUSerial(v, input.Serial)
	v.Check(len(strings.TrimSpace(input.Note)) <= maxLCUNoteLength, "note",
		fmt.Sprintf("must not be more than %d characters", maxLCUNoteLength))
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)
	unit := &data.LCUUnit{
		Serial:   input.Serial,
		StartsOn: data.Today(),
		Note:     input.Note,
		AddedBy:  &user.ID,
	}
	if err := app.models.LCU.Insert(unit); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// Read it back so the response carries the seven-day log the page expects,
	// rather than a half-built unit.
	created, err := app.models.LCU.Get(unit.ID)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusCreated, envelope{"unit": created}, nil)
}

// getLCUUnitHandler returns one reader with its log. The owner or a manager only:
// a reader's week of results is nobody else's business.
func (app *application) getLCUUnitHandler(w http.ResponseWriter, r *http.Request) {
	unit, ok := app.lcuUnitForRequest(w, r)
	if !ok {
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"unit": unit}, nil)
}

// lcuUnitForRequest loads the :id unit and enforces that this user may see it,
// writing the error response itself when they may not.
func (app *application) lcuUnitForRequest(w http.ResponseWriter, r *http.Request) (*data.LCUUnit, bool) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return nil, false
	}
	unit, err := app.models.LCU.Get(id)
	if err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return nil, false
	}
	if !canAnswerFor(app.contextGetUser(r), unit) {
		app.notPermittedResponse(w, r)
		return nil, false
	}
	return unit, true
}

// recordLCUTestsHandler stores the day's result for one or more readers.
//
// It takes a list because someone trialling two readers should answer both in
// one go, and a half-answered prompt is worse than none. Each entry names its
// own day and defaults to today, which is what lets a manager fill in a day
// somebody missed.
func (app *application) recordLCUTestsHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Results []struct {
			UnitID int64  `json:"unit_id"`
			Result string `json:"result"`
			Note   string `json:"note"`
			Day    string `json:"day"`
		} `json:"results"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()
	v.Check(len(input.Results) > 0, "results", "must contain at least one result")
	v.Check(len(input.Results) <= maxLCUResultsPerRequest, "results",
		fmt.Sprintf("must not contain more than %d results", maxLCUResultsPerRequest))
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)
	today := data.Today()

	// Validate everything before writing anything, so a bad entry half way
	// through the list cannot leave some readers answered and others not.
	type pending struct {
		unitID int64
		day    time.Time
		result string
		note   string
	}
	var writes []pending
	seen := make(map[int64]bool, len(input.Results))

	for i, entry := range input.Results {
		field := func(suffix string) string {
			return fmt.Sprintf("results.%d.%s", i, suffix)
		}
		v.Check(entry.UnitID >= 1, field("unit_id"), "must be a valid id")
		data.ValidateLCUResult(v, entry.Result)
		note := strings.TrimSpace(entry.Note)
		v.Check(len(note) <= maxLCUNoteLength, field("note"),
			fmt.Sprintf("must not be more than %d characters", maxLCUNoteLength))
		v.Check(!seen[entry.UnitID], field("unit_id"), "listed more than once")
		seen[entry.UnitID] = true

		day := today
		if entry.Day != "" {
			parsed, err := time.Parse("2006-01-02", entry.Day)
			if err != nil {
				v.AddError(field("day"), "must be a date in YYYY-MM-DD form")
				continue
			}
			day = data.DateOnly(parsed)
		}
		writes = append(writes, pending{unitID: entry.UnitID, day: day, result: entry.Result, note: note})
	}
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	// Permissions and window membership need the units, so they are checked
	// before the first write as well.
	for _, wr := range writes {
		unit, err := app.models.LCU.Get(wr.unitID)
		if err != nil {
			switch {
			case errors.Is(err, data.ErrRecordNotFound):
				v.AddError("unit_id", fmt.Sprintf("no LCU test unit with id %d", wr.unitID))
			default:
				app.serverErrorResponse(w, r, err)
				return
			}
			continue
		}
		if !canAnswerFor(user, unit) {
			app.notPermittedResponse(w, r)
			return
		}
		if wr.day.Before(data.DateOnly(unit.StartsOn)) || wr.day.After(data.DateOnly(unit.EndsOn)) {
			v.AddError("day", fmt.Sprintf(
				"must fall within the reader's test window (%s to %s)",
				data.DateOnly(unit.StartsOn).Format("2006-01-02"),
				data.DateOnly(unit.EndsOn).Format("2006-01-02")))
			continue
		}
	}
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	recorded := make([]*data.LCUTest, 0, len(writes))
	for _, wr := range writes {
		test, err := app.models.LCU.RecordTest(wr.unitID, wr.day, wr.result, wr.note, user.ID)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
		recorded = append(recorded, test)
	}

	// Read the units back so the caller gets the updated log and the unit's
	// verdict if this was the last day.
	out := make([]*data.LCUUnit, 0, len(writes))
	for _, wr := range writes {
		unit, err := app.models.LCU.Get(wr.unitID)
		if err != nil {
			app.serverErrorResponse(w, r, err)
			return
		}
		out = append(out, unit)
	}

	app.writeJSON(w, http.StatusCreated, envelope{
		"tests": recorded,
		"units": out,
		"today": today.Format("2006-01-02"),
	}, nil)
}

// deleteLCUUnitHandler drops a reader that was never going to be worth testing,
// taking its log with it. The owner or a manager only.
func (app *application) deleteLCUUnitHandler(w http.ResponseWriter, r *http.Request) {
	unit, ok := app.lcuUnitForRequest(w, r)
	if !ok {
		return
	}
	if err := app.models.LCU.Delete(unit.ID); err != nil {
		if errors.Is(err, data.ErrRecordNotFound) {
			app.notFoundResponse(w, r)
			return
		}
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{"deleted": true}, nil)
}
