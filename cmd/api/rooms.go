package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// listRoomsHandler returns every run in the inventory, plus how many rooms those
// runs add up to.
//
// The total is a separate number from the row count on purpose: a manager looking
// at seven rows wants to know whether that is seven rooms or three hundred, and
// only the second is the answer to "have I listed the whole hotel".
func (app *application) listRoomsHandler(w http.ResponseWriter, r *http.Request) {
	rooms, err := app.models.Rooms.List()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if rooms == nil {
		rooms = []*data.Room{}
	}

	total, err := app.models.Rooms.CountRooms()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"rooms":        rooms,
		"total_rooms":  total,
		"total_runs":   len(rooms),
		"max_run_size": data.MaxRoomRun,
	}, nil)
}

// searchRoomsHandler backs the typeahead on the apartment-issue form.
//
// It answers with room numbers rather than runs, because that is what the field
// is completing: someone typing "12" wants 1201 and 1214 offered to them, not the
// runs those rooms happen to belong to.
//
// Matching is from the left of the number only. "12" finds rooms that begin 12 --
// not 2412 or 4512, which contain those digits further along and are not the room
// being looked for. On a hotel with several floors that is the difference between
// a list of the floor asked about and a list of every room that happens to share
// two digits with it.
//
// An empty query returns nothing rather than everything. A dropdown that opens on
// three hundred rooms the moment the field is focused is not a list a technician
// can pick from, and the field is empty precisely when they have not decided yet.
func (app *application) searchRoomsHandler(w http.ResponseWriter, r *http.Request) {
	query := app.readString(r.URL.Query(), "q", "")

	found, more, err := app.models.Rooms.Search(query, data.RoomSearchLimit)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if found == nil {
		found = []int{}
	}

	app.writeJSON(w, http.StatusOK, envelope{"rooms": found, "more": more}, nil)
}

// createRoomHandler adds a room or a range of them.
//
// Rooms already in the inventory are skipped rather than refused, so adding a
// range that partly exists tops up the missing part and says how many of each
// there were. The response carries added_rooms and skipped_rooms rather than a
// bare count, because a manager who asked for twenty rooms and got eight needs to
// be told that twelve were already there rather than left to wonder whether the
// other twelve silently failed.
//
// The body is the same string a manager typed into the field, not a pair of
// numbers, so the rules about what counts as a range live in exactly one place --
// data.ParseRoomRun -- and the field and the API cannot disagree about it.
func (app *application) createRoomHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Run string `json:"run"`
	}
	if err := app.readJSON(w, r, &input); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	v := validator.New()

	from, to, err := data.ParseRoomRun(input.Run)
	if err != nil {
		v.AddError("run", err.Error())
		app.failedValidationResponse(w, r, v.Errors)
		return
	}
	if from < 1 {
		v.AddError("run", "room numbers start at 1")
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)
	createdBy := user.ID

	result, err := app.models.Rooms.Add(from, to, &createdBy)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// A range that was already fully covered is not a failure. The request was
	// understood and the inventory already satisfies it, so this is a 200 and the
	// counts say why nothing was written. Answering 201 would claim a room was
	// created when none was, and a 4xx would call it an error when the manager
	// asked for something the system already agreed to.
	status := http.StatusCreated
	if result.Added == 0 {
		status = http.StatusOK
	}

	total, err := app.models.Rooms.CountRooms()
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, status, envelope{
		"rooms":         result.Rooms,
		"added_rooms":   result.Added,
		"skipped_rooms": result.Skipped,
		"total_rooms":   total,
	}, nil)
}

// deleteRoomHandler removes one run.
//
// Apartment issues naming rooms inside it are left as they are and keep their
// stored room number, the same way an issue whose department has been deleted
// keeps its department name. Refusing the delete instead would mean a manager
// could not correct a range they had added by mistake until every issue in those
// rooms had been re-filed somewhere else first.
func (app *application) deleteRoomHandler(w http.ResponseWriter, r *http.Request) {
	id, err := app.readIDParam(r)
	if err != nil {
		app.notFoundResponse(w, r)
		return
	}

	if err := app.models.Rooms.Delete(id); err != nil {
		switch {
		case errors.Is(err, data.ErrRecordNotFound):
			app.notFoundResponse(w, r)
		default:
			app.serverErrorResponse(w, r, err)
		}
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{"message": "room range successfully deleted"}, nil)
}

// resolveRoom returns the canonical room number for an apartment issue, or an
// error naming what is wrong.
//
// It is the same shape as resolveDepartment and for the same reason: the
// inventory is enforced on the way in, but an issue that already exists keeps the
// room it was logged against even if that room has since been removed from the
// inventory. Otherwise deleting a range by mistake would lock the technician out
// of editing every issue filed in those rooms, which is a far worse outcome than
// a stale room number on a historical record.
//
// allowCurrent is the room already stored on the issue being edited. On create it
// is zero, so nothing is allowed through.
func (app *application) resolveRoom(name string, allowCurrent int) (int, error) {
	// Trimmed before parsing, so " 1305 " and "1305" are the same room. That is
	// the whole reason a room is resolved to a number and written back: without it
	// the padding a person typed in one field becomes part of the room's identity
	// and the same room acquires two spellings.
	room, err := strconv.Atoi(strings.TrimSpace(name))
	if err != nil || room < 1 {
		return 0, errors.New("not a room number")
	}

	known, err := app.models.Rooms.Contains(room)
	if err != nil {
		return 0, err
	}
	if known {
		return room, nil
	}
	if allowCurrent != 0 && allowCurrent == room {
		return room, nil
	}
	return 0, errors.New("not in the room inventory")
}
