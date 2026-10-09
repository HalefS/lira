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
// The last clause is load-bearing, not decoration. rota_position is deliberately
// NOT DENSE (000033 explains why), so two placed members can carry the same value
// -- a re-used ordinal and a retained one. Without the id tiebreak the order of
// those two rows would be whatever the planner felt like, which would make the
// workbook's row order unstable across identical requests. ids are unique, so
// adding this clause is what turns "almost always deterministic" into "always".
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

// SetOrder records the order a manager placed members in, in the order given.
//
// A SEPARATE WRITE FROM Set, and the separation is the point. Set's table carries
// updated_at and updated_by as the audit trail of a deliberate EXCLUSION, and 000032
// keeps false rows forever precisely so that trail survives. Folding order into the
// roster write would re-stamp both on every drag of a row, so "when did we change
// the board and who" would decay into "when did anybody last nudge a row", and the
// exclusion history would be unrecoverable for want of a tidier table. Order is a
// different fact about a different table, so it gets its own endpoint.
//
// # WHY THERE IS NO LOCK HERE, WHERE Set TAKES ONE
//
// Set needs LOCK TABLE rota_members IN EXCLUSIVE MODE because it is a read-modify-
// write spread over TWO statements: under READ COMMITTED two concurrent roster
// writes interleave inside the transaction and settle on a roster neither manager
// asked for. That method's comment writes the interleaving out step by step, and it
// is a real hazard that deserves a lock.
//
// This is ONE statement, and a single UPDATE cannot interleave with another UPDATE.
// Two managers reordering at the same moment therefore settle on one manager's order
// IN ITS ENTIRETY -- last-write-wins on a whole-list replace, which is exactly what
// shift_assignments already settles such races with, and which is an answer at least
// one of the two managers actually asked for.
//
// A lock here would prevent nothing. A lock whose justification is "it seemed safer"
// is worse than no lock at all, because it is a claim about correctness that nobody
// has checked and that the next reader has to take on trust.
//
// Members NOT NAMED are left exactly as they are. Their slot is held, not cleared,
// and that is what "their place is kept" means: removing somebody from the rota must
// not silently renumber everybody above them. See migration 000033 for why the stored
// positions are consequently allowed to contain gaps and collisions -- the short
// version being that order is the only thing this column is ever used for.
func (m RotaRosterModel) SetOrder(ctx context.Context, userIDs []int64) error {
	// The empty-versus-nil belt to ValidateRosterOrder's braces, for the same reason:
	// a nil slice binds as SQL NULL, unnest(NULL) yields no rows, and the write would
	// report success having changed nothing. Here the failure is quieter than Set's,
	// so it is worth saying out loud rather than relying on the handler alone.
	if userIDs == nil {
		userIDs = []int64{}
	}

	// WITH ORDINALITY numbers the submitted list 1..N in the order it arrived, which
	// is the whole of what "the order a manager chose" means on the wire. There is no
	// client-side re-sort and no client-side numbering: the client sends a sequence of
	// ids and the database decides what order means, once, here.
	//
	// No transaction wrapper. A single statement is atomic on its own, and wrapping
	// it would buy nothing but a second round trip.
	_, err := m.DB.ExecContext(ctx, `
		UPDATE users u
		SET rota_position = o.ord::integer
		FROM unnest($1::bigint[]) WITH ORDINALITY AS o(id, ord)
		WHERE u.id = o.id`, pq.Array(userIDs))
	return err
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
// An EMPTY order is LEGAL, and unlike an empty roster it needs no confirmation from
// the caller. Empty roster means "nobody is on the rota", which is a decision with a
// visible consequence on a public board and is worth one acknowledgement. Empty order
// means "I have no opinion about anybody", which is precisely the state a fresh
// installation is already in, so it is a no-op rather than a decision.
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
