package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/HalefS/lira/internal/data"
	"github.com/HalefS/lira/internal/validator"
)

// Rota membership: WHO the rota is for, as opposed to who is on which day.
//
// This file is deliberately separate from schedule.go, because its ACCESS is the
// opposite of the rota grid's and that difference is the whole reason it needs saying:
//
//   - GET /v1/schedule is public, with no requireAuth at all, because "who is on
//     tonight" belongs on a noticeboard anybody can walk past. That payload DOES leak
//     the included set -- it has to, it is the list of rows -- so publishing the roster
//     adds nothing anybody could not already read off the grid.
//   - This file's payload is the OTHER half: every account, including the deactivated
//     leavers the grid renders muted, plus how many cells each one holds. That is an
//     account roster, it has no noticeboard justification at all, and it is the input a
//     WRITE needs. So the read is requireAuth and the write is manager-only.
//
// Why the read is requireAuth and not requireManager: GET /v1/users is already
// requireAuth and returns every account with strictly MORE -- email, avatar_data,
// language, must_reset_password. A manager-only roster read would therefore protect
// nothing that is not already exposed to any signed-in technician, and it would put the
// "who may see the user list" rule in one file and the "who may see the roster" rule in
// another. One concept, one place.
//
// Why the URL is /v1/rota/members and not /v1/schedule/roster: httprouter PANICS at
// startup if a static segment and a named parameter share a parent path in the same
// method's routing tree, in EITHER registration order, and /v1/schedule/:user_id already
// exists. Verified against httprouter v1.3.0 rather than believed:
//
//	static after  the wildcard: panic: 'roster' in new path '/v1/schedule/roster'
//	                           conflicts with existing wildcard ':user_id'
//	static before the wildcard: panic: wildcard route ':user_id' conflicts with
//	                           existing children
//
// It would surface as a boot crash inside New(), before the listener binds, and look
// like a broken deploy rather than a routing mistake. A fresh first segment has no such
// rule to depend on, and it is also the more honest shape: the roster is a resource the
// grid READS, not a sub-resource of it.

// rosterInput is the body of a roster write.
//
// confirmEmpty is required only when member_ids is empty, and it is not a validation
// rule -- the client decides whether to ask, and the server decides whether to insist.
//
// The reason it exists at all: the likeliest way an empty roster actually arrives is a
// client whose member fetch failed and submitted [] anyway. "The manager blanked the
// board" and "the client lost the list" are indistinguishable on the wire and not at all
// on the noticeboard, so the empty case has to be acknowledged deliberately. It does
// nothing about the one-at-a-time path, which is four deliberate clicks a manager cannot
// take by accident.
type rosterInput struct {
	MemberIDs    []int64 `json:"member_ids"`
	ConfirmEmpty bool    `json:"confirm_empty"`
}

// listRotaMembersHandler returns every account, on the rota or not, for the picker.
//
// The ordering is included-first and is produced by the query rather than by a client
// sort, so the picker's two sections are a contiguous slice of this list and cannot
// drift from what was stored.
func (app *application) listRotaMembersHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	members, err := app.models.RotaMembers.List(ctx)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	app.writeJSON(w, http.StatusOK, envelope{
		"members":     members,
		"can_edit":    app.canEditSchedule(r),
		"max_members": data.MaxRotaMembers,
	}, nil)
}

// setRotaMembersHandler replaces the roster with exactly the members named.
//
// COLLECTION REPLACE, matching how the rota's own cells are written: the body is the
// whole desired state, so there is nothing partial for a second manager to interleave
// with and therefore no version guard. Race safety is an exclusive table lock inside
// Set, and the interleaving it prevents is written out in that method's comment -- the
// short version being that two managers saving overlapping rosters under READ COMMITTED
// otherwise settle on a roster NEITHER of them asked for, which is not even the
// last-write-wins that shift_assignments deliberately gets.
func (app *application) setRotaMembersHandler(w http.ResponseWriter, r *http.Request) {
	var in rosterInput
	if err := app.readJSON(w, r, &in); err != nil {
		app.badRequestResponse(w, r, err)
		return
	}

	// Shape before existence, for the reason createAbsenceHandler gives: a nil
	// member_ids is a malformed field and deserves a 422 that names it, and checking
	// existence first would leave ValidateRoster's nil rule unreachable over HTTP. A
	// rule no request can exercise is a rule nobody maintains.
	v := validator.New()
	data.ValidateRoster(v, in.MemberIDs)
	if !v.Valid() {
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	if len(in.MemberIDs) == 0 && !in.ConfirmEmpty {
		v.AddError("confirm_empty", "must be true to put nobody on the rota")
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Every named id has to be a real account, checked in ONE query -- a roster write
	// names as many people as the hotel has staff, and one Users.Get per id is the N+1
	// shape this codebase has already unpicked twice. Reported as one 422 naming all of
	// them, which is a shape a manager can act on, rather than an FK violation surfacing
	// as a 500 from inside the transaction.
	found, err := app.models.Users.ExistingIDs(ctx, in.MemberIDs)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	if missing := missingMemberIDs(in.MemberIDs, found); len(missing) > 0 {
		v.AddError("member_ids", fmt.Sprintf("no such member: %s", joinIDs(missing)))
		app.failedValidationResponse(w, r, v.Errors)
		return
	}

	user := app.contextGetUser(r)
	if err := app.models.RotaMembers.Set(ctx, in.MemberIDs, &user.ID); err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}

	// THE RE-READ, not an echo of what was submitted, and not an optimistic patch. The
	// roster on screen after a save has to be what the database says it is, because
	// that is the only version that cannot be wrong -- and because COALESCE means a row
	// this request never touched is what decides "on the rota" for most accounts, so
	// the client could not derive the answer correctly even if it wanted to.
	members, err := app.models.RotaMembers.List(ctx)
	if err != nil {
		app.serverErrorResponse(w, r, err)
		return
	}
	app.writeJSON(w, http.StatusOK, envelope{
		"members":     members,
		"can_edit":    app.canEditSchedule(r),
		"max_members": data.MaxRotaMembers,
	}, nil)
}

// missingMemberIDs returns the ids that were named but are not accounts, in the order
// they were sent.
//
// Order is the sent order rather than sorted, so the message reads back the way the
// request was written and a manager can match it against what they ticked.
func missingMemberIDs(want []int64, found map[int64]bool) []int64 {
	var missing []int64
	for _, id := range want {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

// joinIDs renders ids for an error message: "12, 15, 19".
func joinIDs(ids []int64) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ", "
		}
		out += fmt.Sprintf("%d", id)
	}
	return out
}
