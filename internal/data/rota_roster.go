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

// rotaMemberOrder is the ONE way this codebase sorts rota members, and it exists as a
// constant because exactly two queries need it and they must never disagree:
//
//	ScheduleModel.Week  -- the PUBLIC grid's row order
//	RotaRosterModel.List -- the settings picker's order, included-first then this
//
// The reason that is not a coincidence to be watched for is the attendance workbook.
// internal/report/attendance.go builds one row per member by iterating w.Members in
// slice order, so the XLSX and PDF row order is Week()'s ORDER BY verbatim. A second
// copy of this clause in either query would be a spreadsheet whose rows silently
// stopped matching the grid it was exported from -- invisible in the app, glaring in
// the document a manager hands to payroll.
//
// The clauses, in precedence order, with the reasons they are in that order:
//
//	(u.rota_position IS NULL)  placed members first, never-placed ones after. This
//	                           is the tier separator, and it is an IS NULL test
//	                           rather than a sentinel because NULL is the real
//	                           "no opinion" value and Postgres sorts NULLs last
//	                           within ASC anyway -- the explicit test makes the
//	                           intent readable instead of relying on that.
//	u.rota_position            the order the manager chose, ascending.
//	u.active DESC              deactivation sinks you -- but only if nobody has
//	                           placed you by hand. Placed beats active, on purpose:
//	                           see migration 000033, which argues it at length.
//	u.created_at ASC           the historical default, for everyone unplaced.
//	u.id ASC                   makes the sort TOTAL.
//
// The last clause is load-bearing, not decoration, and NOT for the reason it used to be.
//
// It was originally justified as protection against two placed members carrying the same
// rota_position. That state was reachable -- SetOrder was a partial replace, so two
// sequential writes could collide -- and it is now structurally impossible, because
// SetOrder clears every unnamed position before assigning 1..N in one transaction. So
// the clause no longer earns its keep from rota_position at all.
//
// It earns it from the other four clauses. users.created_at is timestamp(0), so two
// accounts registered in the same second tie, and every one of them has rota_position
// NULL and the same active flag. Without a unique final key those rows would come back
// in whatever order the planner produced, which would make the PUBLIC grid's row order
// -- and therefore the exported workbook's -- unstable across identical requests. ids
// are unique, so appending this clause is what turns "almost always deterministic" into
// "always".
//
// Keep it even if a future migration makes rota_position NOT NULL, and say why here
// rather than deleting a clause that looks redundant to whoever reads it next.
const rotaMemberOrder = `(u.rota_position IS NULL), u.rota_position, ` +
	`u.active DESC, u.created_at ASC, u.id ASC`

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
//
// RotaPosition is NULLABLE and its NULL is meaningful, unlike every other field here:
// it means "no manager has an opinion about where this person goes". It is sent to
// the client as a pointer's worth of absence so the picker can render a dash rather
// than invent a number, because a number in that gutter would be a claim the grid is
// not honouring. See migration 000033.
type RotaRosterEntry struct {
	UserID        int64  `json:"user_id"`
	Name          string `json:"name"`
	AvatarIdx     int    `json:"avatar_idx"`
	Role          string `json:"role"`
	Active        bool   `json:"active"`
	OnRota        bool   `json:"on_rota"`
	AssignedCells int    `json:"assigned_cells"`
	RotaPosition  *int64 `json:"rota_position"`
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
// is rotaMemberOrder -- the SAME clause the grid sorts by -- so a person does not
// appear to move when they are added to the rota, and so a manager reordering in the
// picker sees the numbers they are about to produce rather than a local sort that
// disagrees with the grid the numbers are for.
func (m RotaRosterModel) List(ctx context.Context) ([]*RotaRosterEntry, error) {
	rows, err := m.DB.QueryContext(ctx, `
		SELECT u.id, u.name, u.avatar_idx, u.role, u.active,
		       COALESCE(rm.included, true) AS on_rota,
		       (SELECT COUNT(*) FROM shift_assignments sa WHERE sa.user_id = u.id),
		       u.rota_position
		FROM users u
		LEFT JOIN rota_members rm ON rm.user_id = u.id
		ORDER BY on_rota DESC, `+rotaMemberOrder)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*RotaRosterEntry{}
	for rows.Next() {
		e := &RotaRosterEntry{}
		if err := rows.Scan(&e.UserID, &e.Name, &e.AvatarIdx, &e.Role,
			&e.Active, &e.OnRota, &e.AssignedCells, &e.RotaPosition); err != nil {
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

// SetOrder replaces the whole rota order with exactly userIDs, in the order given, and
// fails if any named member does not exist.
//
// COLLECTION REPLACE, not a patch: the body IS the desired end state, and everybody not
// named goes back to having no position. This is Set's own shape and for Set's own
// reasons -- there is no partial row for a second manager to interleave with, so there
// is no version guard to add.
//
// A SEPARATE WRITE FROM Set, and the separation is the point. Set's table carries
// updated_at and updated_by as the audit trail of a deliberate EXCLUSION, and 000032
// keeps false rows forever precisely so that trail survives. Folding order into the
// roster write would re-stamp both on every drag of a row, so "when did we change the
// board and who" would decay into "when did anybody last nudge a row", and the exclusion
// history would be unrecoverable for want of a tidier table. Order is a different fact
// about a different table, so it gets its own endpoint.
//
// # WHY THIS TAKES A LOCK, WHERE IT PREVIOUSLY TOOK NONE
//
// It did not take one, on the argument that a single UPDATE cannot interleave with
// another UPDATE. That argument was sound about the STATEMENT and wrong about the
// OPERATION, because a partial replace is not a whole-list replace at all:
//
//	UPDATE ... WHERE u.id = o.id          -- touches only the rows you named
//
// Two well-formed calls therefore do NOT settle on one manager's order. Run them in
// sequence, with no concurrency of any kind:
//
//	PUT [1,2,3,87]   ->  1=1, 2=2, 3=3, 87=4
//	PUT [87,89,90]   ->  87=1, 89=2, 90=3
//
// and three accounts now SHARE each position: {1,87} at 1, {2,89} at 2, {3,90} at 3.
// The sort still returns a total order -- u.id breaks the tie, so nothing is
// non-deterministic -- but no manager ever chose that order, and the next reorder
// produces a different answer.
//
// Clearing first and assigning second makes this a genuine two-statement read-modify-
// write, which is precisely the shape Set needs its lock for. Under READ COMMITTED two
// of them interleave inside the transaction:
//
//  1. A clears every position
//  2. B clears every position        -- a no-op by now, but it happened
//  3. A assigns 1=1, 2=2
//  4. B assigns 87=1
//
// and the board ends up holding BOTH managers' orders, because B's clear ran before A's
// assign. That is not last-write-wins; it is an answer neither manager asked for, and
// it is the same class of failure Set's EXCLUSIVE lock exists to prevent.
//
// SHARE ROW EXCLUSIVE rather than the EXCLUSIVE Set takes on rota_members: both block
// other writers, neither blocks plain readers, so the public grid keeps serving while
// two managers reorder. The weaker mode is chosen because this lock contends with
// ordinary user-account writes, which are frequent, and blocking those for the length of
// a two-statement transaction is a wider blast radius than the hazard needs.
func (m RotaRosterModel) SetOrder(ctx context.Context, userIDs []int64) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}

	// The empty-versus-nil belt, and here it is LOAD-BEARING rather than decorative.
	//
	// `NOT (u.id = ANY($1))` is TRUE for every row when $1 is an empty array, so an empty
	// submit clears everybody -- which is the documented meaning of an empty order. With
	// $1 NULL the same expression is NULL, matches nothing, and the CLEAR silently does
	// nothing; the assign then matches nothing either, and the write returns 200 having
	// changed nothing at all.
	//
	// That is the third time this codebase has been bitten by NULL-versus-'{}'. The
	// empty case now carries real meaning, which is what makes the difference between
	// "put the whole board back to the default order" and "report success while doing
	// nothing" the difference between a feature and a bug.
	if userIDs == nil {
		userIDs = []int64{}
	}

	// 1. Everybody not named goes back to having no position.
	//
	//    This is what makes the operation a whole-list replace rather than a partial one,
	//    and what makes colliding positions STRUCTURALLY IMPOSSIBLE rather than merely
	//    unlikely: after statement 2, every non-NULL position was written by this same
	//    transaction from a 1..N ordinality, so no two of them can agree.
	//
	//    The `rota_position IS NOT NULL` guard keeps the UPDATE off rows already in their
	//    desired state, which on an ordinary save is nearly all of them -- so this writes
	//    a handful of tuples instead of the whole user table on every reorder.
	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET rota_position = NULL
		WHERE rota_position IS NOT NULL AND NOT (id = ANY($1))`,
		pq.Array(userIDs)); err != nil {
		return err
	}

	// 2. WITH ORDINALITY numbers the submitted list 1..N in the order it arrived, which
	//    is the whole of what "the order a manager chose" means on the wire. No client-side
	//    re-sort and no client-side numbering: the client sends a sequence of ids and the
	//    database decides what order means, once, here.
	//
	//    ord is bigint and rota_position is integer, hence the cast. Postgres ERRORS on
	//    an out-of-range narrowing rather than truncating silently, and it is unreachable
	//    regardless -- ValidateRosterOrder bounds the list at MaxRotaMembers.
	//
	//    unnest DEDUPLICATES, incidentally: the same id submitted three times updates that
	//    row once, to whichever ordinality wins, which is plan-dependent. That is why
	//    ValidateRosterOrder refuses duplicates rather than trusting this to be harmless.
	res, err := tx.ExecContext(ctx, `
		UPDATE users u
		SET rota_position = o.ord::integer
		FROM unnest($1::bigint[]) WITH ORDINALITY AS o(id, ord)
		WHERE u.id = o.id`, pq.Array(userIDs))
	if err != nil {
		return err
	}

	// THE EXISTENCE CHECK, done where it cannot go stale. The handler already ran
	// ExistingIDs, but that is a SEPARATE transaction, so an account deleted between the
	// check and this write leaves statement 2 matching nothing for it -- and the request
	// returns 200 carrying an order that is quietly missing somebody.
	//
	// RowsAffected is trustworthy here precisely because Postgres counts a row as updated
	// even when the value written equals the value already there, so an unchanged row
	// still counts. That was checked rather than assumed, because a guard that miscounts
	// on the common path is worse than no guard at all.
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != int64(len(userIDs)) {
		return fmt.Errorf("rota order: %d of %d named members do not exist",
			int64(len(userIDs))-affected, len(userIDs))
	}

	return tx.Commit()
}

// WHY THERE IS NO AUDIT TRAIL ON THE ORDER, WHICH IS A DECISION AND NOT AN OVERSIGHT
//
// rota_members keeps updated_at and updated_by forever so that "who took Marco off the
// board, and when" survives. The order has neither, so "who put Marco first" is
// unanswerable afterwards.
//
// That is deliberate, and it matches shift_assignments -- the other write in this
// feature area, which also carries no revision history. The line being drawn is between
// what a rota IS and how it is ARRANGED. Membership is a decision about a person with
// consequences for their shifts; order is presentational. A payroll dispute is about
// hours worked, which live in shift_assignments, and nobody disputes the order of a
// list. If that judgement is ever wrong the fix is two nullable columns on users and one
// more statement here -- no migration would be needed, which is the main reason for not
// pre-building it.
//
// users.version is settled here too. Every other write to users bumps it behind an
// optimistic guard, and this one deliberately does not: it writes a single column that
// is not carried on the User struct, so no client can observe a stale value through the
// API, and bumping it would make an unrelated concurrent account edit fail its version
// guard for a change the caller did not make and cannot see.

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
	validateIDList(v, "member_ids", memberIDs)
}

// ValidateRosterOrder checks a submitted ORDER before anything is written.
//
// It is the same shape of rule as ValidateRoster -- a bounded, duplicate-free list of
// real ids -- over a different field, so both delegate to one implementation rather
// than each carrying their own copy. That is not tidiness for its own sake: these two
// rules drift apart silently when they are duplicated, and the drift shows up as a
// write one endpoint refuses and the other accepts, which is the sort of difference
// nobody notices until a manager has two versions of the app.
//
// The nil refusal carries the same weight as ValidateRoster's and for the same
// reason: SetOrder binds the slice through pq.Array, a nil becomes SQL NULL, and
// unnest(NULL) produces no rows -- so the request would return 200 having changed
// nothing at all.
//
// An EMPTY order is LEGAL, and unlike an empty roster it needs no confirmation from the
// caller -- because it is not the same kind of decision.
//
// An empty ROSTER means "nobody is on the rota": a visible change to a public board,
// worth one acknowledgement before it is sent. An empty ORDER means "nobody has a
// place", which is a real change with a real effect -- every position goes back to NULL
// and the grid returns to the historical sort -- so confirming it is the roster
// dialog's job, not this one's, and asking here would put an "are you sure" in front of
// a revert that the manager has usually just been told is safe.
func ValidateRosterOrder(v *validator.Validator, userIDs []int64) {
	v.Check(userIDs != nil, "user_ids", "must be provided")
	if userIDs == nil {
		return
	}
	validateIDList(v, "user_ids", userIDs)
}

// validateIDList is the shared body of ValidateRoster and ValidateRosterOrder.
//
// Split out because the two rules are the same rule over two fields, and because a
// second hand-written copy of "bounded, positive, no duplicates" is a second thing to
// keep correct. field names the error key so each caller reports against the field its
// own request actually carries.
func validateIDList(v *validator.Validator, field string, ids []int64) {
	v.Check(len(ids) <= MaxRotaMembers, field,
		fmt.Sprintf("must not contain more than %d members", MaxRotaMembers))

	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id < 1 {
			v.AddError(field, fmt.Sprintf("%d is not a valid member id", id))
			continue
		}
		if seen[id] {
			v.AddError(field, fmt.Sprintf("member %d is listed more than once", id))
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
