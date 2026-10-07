package data

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/HalefS/lira/internal/validator"
	"github.com/lib/pq"
)

// Who is on the team rota.
//
// The grid used to print every user account, which meant test accounts, staff of
// another property and accounts belonging to nobody in particular all appeared as rows
// reading "Not on this rota". This file is how a manager says who actually belongs on
// the board.
//
// A MISSING ROW MEANS THE MEMBER IS ON THE ROTA, and that single sentence is the whole
// design. It is what lets migration 000032 insert nothing: an installation that has
// never had a roster carries no rows and reads as "everybody on", so the deploy cannot
// blank a public noticeboard. The alternative -- a set table where an absent row means
// NOT on the rota -- needs a backfill, and a backfill obliges account creation to
// insert a row forever, or the next person to sign up silently fails to appear on the
// board.
//
// What is kept, on purpose and irreversibly:
//
//   - A removed member's shift_assignments. They are NEVER deleted. An assignment row
//     carries no date, so deleting seven of them destroys no fact about when anybody
//     worked -- it deletes configuration, and it is the unrecoverable-typo trap: a
//     manager who removes somebody for a week and adds them back next week must get
//     their week back untouched, not seven empty cells. Re-inclusion is therefore
//     meaningful, and the roster payload counts a removed member's cells so the picker
//     can say so out loud.
//
//   - Their absences. 000031's down migration already argues this for the absences table
//     itself: a recorded absence is a statement somebody made about a named colleague on
//     a specific date, and excluding somebody from a rota is not a statement that their
//     March holiday was wrong.
//
// What is NOT kept: the tri-state. included is NOT NULL, so every reader has exactly
// two answers and never a third.

// MaxRotaMembers bounds one roster write.
//
// Reachable through the API, so this is a real check in ValidateRoster and not only a
// database constraint -- the same distinction ValidateAbsence draws for
// MaxAbsenceSpanDays. It sits far above any real team; it exists to stop a client
// looping on an unbounded list, not because the grid cannot draw that many rows.
const MaxRotaMembers = 200

// RotaRosterEntry is one account as the roster picker needs it.
//
// AssignedCells is read in the same query as everything else, and deliberately is NOT
// derived from len(Days): the picker's whole job in that column is to say "Marco is off
// the rota but still has Tuesday", and a count that had to be re-derived on the client
// could disagree with the grid it is describing.
//
// OnRota is the tri-state COLLAPSED. The SQL sees absent / true / false; one COALESCE
// makes it two answers, so no reader downstream has to know that the common case is
// expressed by the absence of a row.
type RotaRosterEntry struct {
	UserID        int64  `json:"user_id"`
	Name          string `json:"name"`
	AvatarIdx     int    `json:"avatar_idx"`
	Role          string `json:"role"`
	Active        bool   `json:"active"`
	OnRota        bool   `json:"on_rota"`
	AssignedCells int    `json:"assigned_cells"`
}

// RotaRosterModel reads and writes rota membership.
//
// *sql.DB rather than an interface, matching every other model in this package. An
// interface invented for one model is a second abstraction to keep in step with the
// first, and nothing here is mocked -- the tests run against a real database.
type RotaRosterModel struct {
	DB *sql.DB
}

// List returns every account, on the rota or not.
//
// This is the deliberate INVERSE of the grid's own member query, and the asymmetry is
// the point rather than an inconsistency: the grid wants only the included, so it
// filters; the picker has to show everybody so the excluded can be added back, so it
// left-joins and collapses the tri-state. Same table, opposite join, because they
// answer different questions.
//
// Ordered included-first so the picker's two sections are a contiguous slice of this
// list rather than a sort the client has to reproduce. The tiebreak inside each group
// matches the grid's own ordering (active before deactivated, then created_at) so a
// person does not appear to move when they are added to the rota.
func (m RotaRosterModel) List(ctx context.Context) ([]*RotaRosterEntry, error) {
	rows, err := m.DB.QueryContext(ctx, `
		SELECT u.id, u.name, u.avatar_idx, u.role, u.active,
		       COALESCE(rm.included, true) AS on_rota,
		       (SELECT COUNT(*) FROM shift_assignments sa WHERE sa.user_id = u.id)
		FROM users u
		LEFT JOIN rota_members rm ON rm.user_id = u.id
		ORDER BY on_rota DESC, u.active DESC, u.created_at ASC, u.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*RotaRosterEntry{}
	for rows.Next() {
		e := &RotaRosterEntry{}
		if err := rows.Scan(&e.UserID, &e.Name, &e.AvatarIdx, &e.Role,
			&e.Active, &e.OnRota, &e.AssignedCells); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Set replaces the roster with exactly memberIDs, and returns the stored result.
//
// COLLECTION REPLACE, not a patch: the request body IS the desired end state. This is
// why there is no version column anywhere near this table -- see the migration header,
// which records the reasoning so the next person does not add one by muscle memory. It
// is also the same shape as ScheduleModel.SetWeek, whose comment gives the matching
// reason: an assignment is only ever written by a request carrying the caller's whole
// row, so there is no partial row for a second manager to interleave WITH.
//
// An empty memberIDs is LEGAL and means nobody is on the rota. That is the reason the
// table stores a flag instead of being a set; see the header.
//
// The two statements below make every NOT-listed account false, so they are written as
// two steps rather than as a delete-then-insert. A delete-then-insert would throw away
// updated_at on every row it touched and would make the table momentarily empty inside
// the transaction -- which, on a rollback, is a roster nobody chose.
func (m RotaRosterModel) Set(ctx context.Context, memberIDs []int64, by *int64) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Serialise the two statements below against a concurrent roster write.
	//
	// A TRANSACTION ALONE IS NOT ENOUGH HERE, and this line is why. Under READ
	// COMMITTED two concurrent roster writes interleave INSIDE the transaction.
	// Manager A saves {Ana, Bea} and manager B saves {Bea, Cal}:
	//
	//   1. A's INSERT ... ON CONFLICT   -> Ana=true,  Bea=true
	//   2. B's INSERT ... ON CONFLICT   -> Bea=true,  Cal=true
	//   3. A's UPDATE ... NOT = ANY(...)  -> Cal=false
	//   4. B's UPDATE ... NOT = ANY(...)  -> Ana=false
	//
	// The result is {Bea}. That is NEITHER manager's intent, and it is not even the
	// last-write-wins that shift_assignments deliberately settles such races with --
	// it is an answer neither of them asked for.
	//
	// EXCLUSIVE blocks other writers but not plain readers, so the public grid keeps
	// serving normally while two managers save. The lock is transaction-scoped and
	// releases at commit or rollback. The table is bounded by the account count and
	// the write is a manager clicking Save on a picker, so a one-line table lock is
	// the right trade today; if the rota ever grows per-department this becomes the
	// scaling ceiling and moves to a per-department advisory lock.
	if _, err := tx.ExecContext(ctx, `LOCK TABLE rota_members IN EXCLUSIVE MODE`); err != nil {
		return err
	}

	// The empty-versus-nil distinction, settled ONCE and used by both statements
	// below. A nil memberIDs binds as SQL NULL through pq.GenericArray, and
	// `user_id = ANY(NULL)` is NULL rather than true, so both statements would match
	// NOTHING and the request would return 200 having changed nothing at all -- a
	// silently wrong board rather than a visibly empty one, which is the worst of the
	// possible outcomes. ValidateRoster refuses a nil slice before we get here; this is
	// the belt to those braces, because the failure it prevents is invisible.
	if memberIDs == nil {
		memberIDs = []int64{}
	}
	ids := pq.Array(memberIDs)

	// 1. Somebody newly INCLUDED who was previously excluded. This is the only thing
	//    that ever turns a retained exclusion back into "no opinion", and it has to
	//    happen or a re-included person would still read as excluded.
	//
	//    Touching ONLY the false rows is deliberate. An earlier version inserted a row
	//    for every account with included=true, which is behaviourally identical to no
	//    row and wrong in two ways: the table grew to one row per account so the
	//    partial index stopped being the small subset it exists to be, and updated_at
	//    was stamped on accounts nobody touched, destroying the audit trail the
	//    migration keeps false rows for.
	if _, err := tx.ExecContext(ctx, `
		UPDATE rota_members
		SET included = true, updated_at = NOW(), updated_by = $2
		WHERE NOT included AND user_id = ANY($1)`,
		ids, by); err != nil {
		return err
	}

	// 2. Everybody NOT named becomes an exclusion.
	//
	//    The DO UPDATE carries `WHERE rota_members.included`, and that guard is the whole
	//    subtlety. A conflict does not mean "already excluded" -- it means "already has
	//    an opinion", and the opinion on file may be the OPPOSITE one. With a plain
	//    DO NOTHING, an account carrying included = true from an earlier save is left
	//    true when a later save excludes them, and stays on a public noticeboard nobody
	//    chose. The live test caught exactly that: taking the whole roster off left one
	//    of six people still on it.
	//
	//    The guard then does the other half of the job. A row that is ALREADY false
	//    matches no guard, so Postgres skips the update entirely and created_at AND
	//    updated_at are both left alone -- "excluded" did not change, so the date the
	//    decision was first made and last actually altered both survive. A row that is
	//    currently true IS flipped, and that is a real decision, so it earns a fresh
	//    updated_at and updated_by.
	//
	//    An account that is included and was never excluded still gets NO ROW here or
	//    above, so "missing means included" stays the live invariant rather than
	//    depending on which code path last ran.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO rota_members (user_id, included, created_at, updated_at, updated_by)
		SELECT u.id, false, NOW(), NOW(), $2
		FROM users u
		WHERE NOT (u.id = ANY($1))
		ON CONFLICT (user_id) DO UPDATE
		    SET included = false, updated_at = NOW(), updated_by = $2
		    WHERE rota_members.included`,
		ids, by); err != nil {
		return err
	}

	return tx.Commit()
}

// ValidateRoster checks a submitted roster before anything is written.
//
// memberIDs nil is REFUSED rather than treated as empty, and that asymmetry is the
// whole reason this function exists. A nil slice binds as SQL NULL through
// pq.GenericArray, so the exclusion UPDATE in Set would evaluate to NULL and silently
// match nothing: the request would succeed and change nothing. SetWeek and
// TVSwapModel.SetForIssue have both been bitten by this exact NULL-versus-'{}' trap
// already; this is the third place it could have bitten, and the only one where the
// failure mode is a wrong board rather than a wiped one.
//
// An EMPTY slice is legal and means nobody is on the rota. The caller may add a
// confirm_empty acknowledgement on top of that, but emptiness itself is not a
// validation failure -- it is a decision a manager is entitled to make, and the roster
// picker asks about it in words before it sends it.
func ValidateRoster(v *validator.Validator, memberIDs []int64) {
	v.Check(memberIDs != nil, "member_ids", "must be provided")
	if memberIDs == nil {
		return
	}

	v.Check(len(memberIDs) <= MaxRotaMembers, "member_ids",
		fmt.Sprintf("must not contain more than %d members", MaxRotaMembers))

	seen := make(map[int64]bool, len(memberIDs))
	for _, id := range memberIDs {
		if id < 1 {
			v.AddError("member_ids", fmt.Sprintf("%d is not a valid member id", id))
			continue
		}
		if seen[id] {
			v.AddError("member_ids", fmt.Sprintf("member %d is listed more than once", id))
		}
		seen[id] = true
	}
}

// ExistingIDs returns which of the given ids are real accounts.
//
// ONE query rather than one Users.Get per id, because a roster write names as many
// people as the hotel has staff. Missing ids are simply absent from the returned map;
// the handler turns that into a single 422 naming them, which is a shape a manager can
// act on -- rather than an FK violation raised inside the transaction and reported as a
// 500.
func (m UserModel) ExistingIDs(ctx context.Context, ids []int64) (map[int64]bool, error) {
	found := make(map[int64]bool, len(ids))
	if len(ids) == 0 {
		return found, nil
	}
	rows, err := m.DB.QueryContext(ctx,
		`SELECT id FROM users WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		found[id] = true
	}
	return found, rows.Err()
}
