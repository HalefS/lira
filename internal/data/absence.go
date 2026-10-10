package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

// Per-date absences: the dated exceptions to the recurring rota.
//
// WHAT AN ABSENCE IS NOT, unchanged and still the reason this file exists:
//
//   - a day off is a DECISION about the rota. In the standing pattern it is the
//     ABSENCE of a row in shift_assignments, and since 000035 it may be an explicit
//     NULL shift_id in a shift_week_cells row -- "off this week, on every other".
//     Either way it is a plan, it recurs, and it is nobody's business why.
//   - an absence here is a dated span somebody recorded, with a reason kind and a
//     note. It exists once, for specific dates, and disappears from the board when
//     those dates pass.
//
// Conflating the two is the failure this design is built to make impossible:
// "Ana is off Monday" and "Ana is on holiday the 3rd to the 7th" are different
// statements, and a board that renders them identically is lying to its readers.
//
// THE FIRST ARGUMENT FOR THIS FILE IS NOW OBSOLETE, and it is worth being exact
// about which half.
//
// This header used to say absences exist because shift_assignments cannot hold a
// dated exception. That was true, and 000035 made it false: a shift_week_cells row
// carries a week_start and can say anything about any particular week. Nothing was
// built from the claim being true, so nothing here needs changing -- but a comment
// that argues from a constraint the schema has dropped is worse than no comment,
// because the next reader will weigh it.
//
// The other half stands, and it is the half that actually matters. A per-week cell
// is a SCHEDULING decision; an absence is something that HAPPENED to a person --
// they were ill, they were on holiday, they were being trained. Recording a day as
// an absence does not assert that the rota was wrong, and keeping a holiday out of
// the rota's own history is what stops "why was Marco not on?" from becoming a
// question the rota itself appears to answer. Excluding somebody from a rota is
// not a statement that their holiday was wrong, and the redaction of `kind` on the
// public board rests on exactly the same distinction.

// The four kinds, and the set is closed because the four real cases are four real
// cases. Must stay in step with absences_kind_check in migration 000031, the way
// shifts_start_time_check stays in step with data.clockTimePattern.
const (
	AbsenceVacation    = "vacation"
	AbsenceSick        = "sick"
	AbsenceTraining    = "training"
	AbsenceUnavailable = "unavailable"
)

const (
	// MaxAbsenceSpanDays caps one absence at two years. Far past anything real --
	// the longest a hotel employee takes is a month, or a parental leave at five
	// months -- and short enough that a mistyped 20271101 cannot paint somebody off
	// the board until 2031, which on a wall noticeboard is a fact nobody can see
	// past.
	//
	// Unlike MaxShiftMinutes, which its own comment calls unreachable through the
	// API, this cap IS reachable by an ordinary typo. That is the difference
	// between a belt-and-braces limit and a real validation, and it is why the
	// check lives in ValidateAbsence rather than only in a database constraint.
	MaxAbsenceSpanDays = 730

	// MaxAbsenceReasonLength matches the note columns already in this schema
	// (lcu_units.note, maintenance_checks.last_note) rather than inventing a
	// fourth house length.
	MaxAbsenceReasonLength = 200
)

// AbsenceKinds returns the four kinds in the order the picker offers them, so the
// validation message and the button list are the same slice rather than two
// hand-kept lists.
func AbsenceKinds() []string {
	return []string{AbsenceVacation, AbsenceSick, AbsenceTraining, AbsenceUnavailable}
}

var validAbsenceKind = func() map[string]bool {
	m := make(map[string]bool, len(AbsenceKinds()))
	for _, k := range AbsenceKinds() {
		m[k] = true
	}
	return m
}()

// Absence is one dated exception to the recurring rota: a person, why, and the
// inclusive run of dates it covers.
//
// Reason is free text and is the one field here that must never reach an anonymous
// caller -- it is the field somebody types a medical detail into. It does not
// appear in AbsenceSummary, which is the shape that travels in GET /v1/schedule,
// so the leak is prevented by a type boundary rather than by remembering a
// conditional in a handler.
type Absence struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	UserID int64  `json:"user_id"`
	Kind   string `json:"kind"`

	// StartsOn is day one and EndsOn is the LAST day, both inclusive. A one-day
	// absence is the same date twice, which is what a manager typing two dates
	// means. This is the opposite of WeekRange's half-open form, and correctly so:
	// WeekRange's exclusivity is a query bound that never leaves the database.
	StartsOn JSONDate `json:"starts_on"`
	EndsOn   JSONDate `json:"ends_on"`

	// The one field a caller must think about before serving this struct to
	// anybody. See the type's own doc comment and AbsenceSummary.
	Reason    string `json:"reason"`
	CreatedBy *int64 `json:"created_by"`
	UpdatedBy *int64 `json:"updated_by"`
	Version   int    `json:"version"`
}

// DayCount is how many calendar days the span covers, inclusive at both ends, so a
// form can say "14 days" without the client doing the arithmetic -- and getting it
// off by one.
//
// Compared as UTC midnights rather than by dividing a duration by 24h: two local
// midnights are 23 or 25 hours apart in real time across a DST change, and a
// division then loses a day twice a year.
func (a *Absence) DayCount() int {
	s, e := DateOnly(a.StartsOn.Time()), DateOnly(a.EndsOn.Time())
	return int(e.Sub(s)/(24*time.Hour)) + 1
}

// Covers reports whether one of the seven grid dates falls inside the span.
//
// Both arguments and both bounds are compared as YYYY-MM-DD strings rather than as
// time.Time, and that is the whole reason this function is safe: a `date` column
// scanned back from Postgres arrives as midnight UTC, while WeekRange's dates are
// local midnight, so comparing the two instants would put everybody west of UTC a
// day out. Comparing ISO date strings is a calendar comparison that no timezone
// can move.
func (a *Absence) Covers(date string) bool {
	return a.StartsOn.Time().Format(time.DateOnly) <= date &&
		date <= a.EndsOn.Time().Format(time.DateOnly)
}

// ValidateAbsence checks one absence before anything is written: a person, one of
// the four reasons, and a run of dates that runs forwards.
//
// The span rules are the interesting ones. An end before the start is not a
// one-day absence, it is a typo, and lcu_units refuses the same thing for the same
// reason. And the maximum is not about storage -- it is about the noticeboard.
//
// Trims and lower-cases in place, like ValidateShift, so what gets validated is
// what gets stored and the stored kind is always one of the four.
func ValidateAbsence(v *validator.Validator, a *Absence) {
	a.Kind = strings.ToLower(strings.TrimSpace(a.Kind))
	a.Reason = strings.TrimSpace(a.Reason)

	v.Check(a.UserID > 0, "user_id", "must be provided")
	v.Check(a.Kind != "", "kind", "must be provided")
	v.Check(validAbsenceKind[a.Kind], "kind",
		fmt.Sprintf("must be one of %s", strings.Join(AbsenceKinds(), ", ")))
	v.Check(len(a.Reason) <= MaxAbsenceReasonLength, "reason",
		fmt.Sprintf("must not be more than %d characters", MaxAbsenceReasonLength))

	starts, ends := a.StartsOn.Time(), a.EndsOn.Time()

	// The zero time is what a missing date leaves behind. Refusing it here means the
	// rules below never reason about a date that is not one, which would otherwise
	// report "must not be before the start date" about a field that was never sent
	// -- the second complaint about the same missing field that ValidateShift
	// guards against with its early return.
	v.Check(!starts.IsZero(), "starts_on", "must be provided")
	v.Check(!ends.IsZero(), "ends_on", "must be provided")
	if starts.IsZero() || ends.IsZero() {
		return
	}

	v.Check(!ends.Before(starts), "ends_on", "must not be before the start date")
	if ends.Before(starts) {
		return
	}

	span := &Absence{StartsOn: JSONDate(starts), EndsOn: JSONDate(ends)}
	v.Check(span.DayCount() <= MaxAbsenceSpanDays, "ends_on",
		fmt.Sprintf("must not be more than %d days after the start date", MaxAbsenceSpanDays))
}

// AbsenceRef is one absence as the PUBLIC grid carries it.
//
// This type, not Absence, is what GET /v1/schedule sends, and that route has no
// requireAuth on it. So two things are decided by the shape rather than by a
// condition somebody has to remember:
//
//  1. There is NO Reason field. Not omitted, not nulled -- absent from the type,
//     which means there is no code path that can put the note in here by
//     accident. The note is the field somebody types a medical detail into, and a
//     field that is present-but-empty is a field a later refactor starts trusting.
//     A manager who opens the edit dialog fetches the note from
//     GET /v1/absences/:id, which is manager-only. That is one extra request on a
//     rare action, and it buys a guarantee that does not depend on a branch.
//
//  2. It carries the RANGE, not a per-week day. The first draft of this folded
//     absences into seven per-member arrays, which is tidier on the wire and
//     quietly lossy: once a run has been cut into cells for the week on screen,
//     nothing left can say whether it ends on Friday or runs on into next week.
//     A run that started last month and ends on Thursday has to be able to render
//     as three cells here and seven next week, and it has to be able to show a
//     "continues" marker on the Sunday. Ranges do both; cells do neither.
//
// Kind is here but reduced for non-managers by PublicKind. Version is here so a
// manager editing from the grid can PATCH without provoking a 409 against a
// version they never fetched.
type AbsenceRef struct {
	ID       int64    `json:"id"`
	UserID   int64    `json:"user_id"`
	Kind     string   `json:"kind"`
	StartsOn JSONDate `json:"starts_on"`
	EndsOn   JSONDate `json:"ends_on"`
	Version  int      `json:"version"`
}

// Ref is the wire shape. Built from the row, so nothing can be added to the
// response by forgetting to clear a field.
func (a *Absence) Ref() *AbsenceRef {
	return &AbsenceRef{
		ID:       a.ID,
		UserID:   a.UserID,
		Kind:     a.Kind,
		StartsOn: a.StartsOn,
		EndsOn:   a.EndsOn,
		Version:  a.Version,
	}
}

// PublicKind reduces an absence kind to what this caller is allowed to be told.
//
// Sick leave comes back as "unavailable", for everybody who is not a manager.
// The rota is a noticeboard that anybody can read without an account, and
// publishing the word SICK next to a named colleague is publishing health
// information about them -- which is a different and larger thing than publishing
// that they are off.
//
// "unavailable" rather than a fifth invented kind, for two reasons. It is already
// one of the four the application stores, so the client renders exactly four
// glyphs and needs no special case. And two real kinds collapsing onto one label
// is not a loss: from outside the building, a colleague on sick leave and a
// colleague who is simply not available ARE the same fact, and the legend
// deduplicates on the kind the payload carries, so the public board says
// "Unavailable - 2 people" where the manager's says "Sick leave - 1, Unavailable -
// 1".
//
// A manager gets the real kind. The caller passes canEditSchedule(r), which is the
// same flag the response already carries, so the two cannot disagree.
func PublicKind(kind string, canEdit bool) string {
	if canEdit {
		return kind
	}
	if kind == AbsenceSick {
		return AbsenceUnavailable
	}
	return kind
}

// AbsenceModel is the dated exceptions to the rota. Deliberately not a method on
// ScheduleModel: the rota is a recurring pattern with no dates, and a model
// holding both would eventually grow a method that needs a week argument it has no
// other use for.
type AbsenceModel struct {
	DB *sql.DB
}

// absenceColumns is every field in one string, so the scan order cannot drift from
// the select list -- the shiftColumns rule, for the same reason.
const absenceColumns = `
	a.id, a.created_at, a.updated_at, a.user_id, a.kind,
	a.starts_on, a.ends_on, a.reason, a.created_by, a.updated_by, a.version`

// scanAbsence reads one absences row in the order of absenceColumns.
//
// Takes a scanner as a plain func rather than an interface, because it is called
// with both a rows.Scan method value and a row.Scan one -- the reason scanUser,
// scanShift and scanMaintenanceSchedule are written this way.
func scanAbsence(scan func(dest ...any) error, lead ...any) (*Absence, error) {
	var (
		a                    Absence
		createdBy, updatedBy sql.NullInt64
	)
	dest := make([]any, 0, len(lead)+11)
	dest = append(dest, lead...)
	dest = append(dest,
		&a.ID, &a.CreatedAt, &a.UpdatedAt, &a.UserID, &a.Kind,
		&a.StartsOn, &a.EndsOn, &a.Reason, &createdBy, &updatedBy, &a.Version)
	if err := scan(dest...); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		a.CreatedBy = &createdBy.Int64
	}
	if updatedBy.Valid {
		a.UpdatedBy = &updatedBy.Int64
	}
	// A `date` column arrives as midnight UTC. Normalising here means DayCount and
	// Covers can rely on it rather than each remembering to.
	a.StartsOn = JSONDate(DateOnly(a.StartsOn.Time()))
	a.EndsOn = JSONDate(DateOnly(a.EndsOn.Time()))
	return &a, nil
}

// windowPredicate is the one place the overlap test is written.
//
// TWO halves, and the second is the one that gets forgotten: starts_on <= $2 finds
// absences that had begun by the end of the window, and ends_on >= $1 finds those
// that had not finished before it started. The first half alone matches a
// six-month holiday for every date in the future and fails SILENTLY -- the grid
// would show somebody absent indefinitely, with nothing in the logs to notice.
//
// Written once here and interpolated twice, rather than open-coded in each query,
// because "fix it in one place" and "the other one still has it" are the two
// outcomes of copy-paste and only one of them is a fix.
const windowPredicate = `a.starts_on <= $2::date AND a.ends_on >= $1::date`

// InWindow returns every absence touching [from, to], inclusive at both ends, for
// every member at once.
//
// This is the read the grid needs and it is deliberately not one query per member:
// this codebase has already had to unpick N+1 twice (loadIssueChildren,
// LCUModel.testsForUnits) and a rota with absences would be the third.
//
// Takes its context rather than making one, so ScheduleModel.Week can read
// absences under the same deadline as the rest of the grid.
func (m AbsenceModel) InWindow(ctx context.Context, from, to time.Time) ([]*Absence, error) {
	query := `SELECT ` + absenceColumns + `
		FROM absences a
		WHERE ` + windowPredicate + `
		ORDER BY a.user_id, a.starts_on, a.id`

	rows, err := m.DB.QueryContext(ctx, query, DateOnly(from), DateOnly(to))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Absence{}
	for rows.Next() {
		a, err := scanAbsence(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListForUser returns one person's absences touching [from, to], newest window
// first. Unpaged: a person accumulates tens of these in a lifetime and every
// reader wants all of them.
//
// user_id is $3 rather than $1 because windowPredicate owns $1 and $2, and
// rewriting it per query is how two copies of an overlap test drift apart.
func (m AbsenceModel) ListForUser(userID int64, from, to time.Time) ([]*Absence, error) {
	query := `SELECT ` + absenceColumns + `
		FROM absences a
		WHERE a.user_id = $3 AND ` + windowPredicate + `
		ORDER BY a.starts_on DESC, a.id DESC`

	ctx, cancel := listContext()
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, DateOnly(from), DateOnly(to), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Absence{}
	for rows.Next() {
		a, err := scanAbsence(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Get returns one absence, or ErrRecordNotFound.
func (m AbsenceModel) Get(id int64) (*Absence, error) {
	query := `SELECT ` + absenceColumns + ` FROM absences a WHERE a.id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	a, err := scanAbsence(m.DB.QueryRowContext(ctx, query, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	return a, err
}

// Create stores a new absence.
//
// A single statement with no overlap check and no transaction, and that is the
// design rather than an omission. Overlap is legal -- holiday and sick leave
// genuinely collide, and refusing it would mean one of two true facts cannot be
// written -- so there is no invariant to enforce and therefore no check-then-insert
// to make race-safe. The classic trap of locking rows that matched nothing does not
// arise, because there is nothing to lose.
//
// updated_by starts as the creator, matching how maintenance_schedules is written
// on insert.
func (m AbsenceModel) Create(a *Absence) error {
	query := `
		INSERT INTO absences (user_id, kind, starts_on, ends_on, reason, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		RETURNING id, created_at, updated_at, version`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query,
		a.UserID, a.Kind, a.StartsOn.Time(), a.EndsOn.Time(), a.Reason, a.CreatedBy).
		Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt, &a.Version)
	return err
}

// Update changes an absence, guarded by the version it was read at.
//
// Unlike a rota cell, an absence genuinely is a read-modify-write -- extending a
// holiday is an UPDATE of one row -- so it carries a version and two managers can
// hold the same one. The loser gets ErrEditConflict, which the handler reports as
// 409 rather than 404: the record is still there, somebody else saved first, and
// telling the manager it is gone would send them looking for something that is not
// missing. ShiftModel.Update makes the same distinction.
func (m AbsenceModel) Update(a *Absence) error {
	query := `
		UPDATE absences
		SET kind=$1, starts_on=$2, ends_on=$3, reason=$4,
		    updated_at=NOW(), updated_by=$5, version=version+1
		WHERE id=$6 AND version=$7
		RETURNING updated_at, version`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query,
		a.Kind, a.StartsOn.Time(), a.EndsOn.Time(), a.Reason,
		a.UpdatedBy, a.ID, a.Version).
		Scan(&a.UpdatedAt, &a.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrEditConflict
	}
	return err
}

// Delete removes an absence. ErrRecordNotFound when there was no such record.
//
// Note what this does NOT do: it does not touch the rota cells the person held on
// those dates. An absence and an assignment are independent facts, and clearing
// somebody's week because they were off sick for three days would quietly destroy
// the standing pattern behind it.
func (m AbsenceModel) Delete(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, `DELETE FROM absences WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return ErrRecordNotFound
	}
	return nil
}
