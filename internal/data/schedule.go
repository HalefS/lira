package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
	"github.com/lib/pq"
)

// Weekly team shift rota.
//
// The rota is RECURRING, and that one fact decides most of what follows. There is
// no date on an assignment, so there is no week to store and no week to query: the
// same seven rows answer for every week until somebody changes them. The `week`
// parameter on the read endpoint therefore decides only which seven DATES are
// printed across the top of the grid. A test says so explicitly, because "filter
// the assignments by week" is the mistake somebody will otherwise make here and
// it fails silently -- the grid comes back with seven empty cells.
//
// Reused rather than reinvented:
//   - WeekRange is the application's one definition of a Monday-to-Sunday week
//     (consumables_weekly.go). internal/report already walks it to build its seven
//     day-bars.
//   - JSONDate is how a calendar date leaves this application: "2026-11-02", never
//     "2026-11-02T00:00:00Z" (see the reasoning on JSONDate itself).
//   - clockTimePattern, ValidateClockTime and ComputeDurationMinutes are how
//     "HH:MM" is checked and how an end before a start is read as crossing
//     midnight. A night shift is not a new idea here; the issue form has been
//     storing overnight pairs since migration 000007.

const (
	// MaxShiftMinutes caps a shift's length. It is unreachable through the API --
	// the end is always read relative to the start, so the length is between one
	// minute and 24 hours by construction -- and exists so a hand-edited row
	// cannot produce an absurd cell. The same defensive shape as
	// ClampLCUWindowDays.
	MaxShiftMinutes = 24 * 60

	// MaxShiftNameLength matches the other catalogs a manager fills in
	// (issue_types, departments), rather than connecta_agents' more generous 100.
	MaxShiftNameLength = 50

	// MaxAssignmentsPerRequest bounds one PUT /v1/schedule. Seven is the entire
	// grid for one person, so anything beyond it is a client that has lost track
	// of which cells it is saving, and this stops the handler looping over an
	// unbounded list.
	MaxAssignmentsPerRequest = 7
)

// WeekDay is one column of the grid: a weekday, the date that weekday falls on in
// this particular week, and whether it is today.
type WeekDay struct {
	// Weekday is ISO 8601 -- 1 = Monday … 7 = Sunday -- and is the value an
	// assignment is keyed on. Not time.Weekday(), which is Sunday-first and
	// zero-based: two encodings of the same thing where picking the wrong one
	// shifts every column by one without anything looking broken.
	Weekday int      `json:"weekday"`
	Label   string   `json:"label"`
	Date    JSONDate `json:"date"`
	// IsToday marks the column the reader is actually in, so the noticeboard can
	// highlight it without the client working out which of the seven it is.
	IsToday bool `json:"is_today"`
}

// Week is a Monday-to-Sunday week as the grid wants it.
type Week struct {
	// Start is Monday and End is Sunday, both INCLUSIVE, so the pair reads as the
	// week it names. WeekRange's half-open [start, start+7) form is an
	// implementation detail of the range maths and is not what a reader wants to
	// see; the consumables report exposes both because there To is a query bound.
	Start JSONDate  `json:"start"`
	End   JSONDate  `json:"end"`
	Days  []WeekDay `json:"days"`
}

// WeekOf returns the week containing ref, as the grid needs it.
//
// Built on WeekRange so there is exactly one definition of where a week starts in
// this application. The seven days are walked with AddDate rather than by adding
// 7*24h, for the reason the LCU window gives: adding hours silently shortens or
// lengthens a run of calendar days across a DST transition, and a rota is a run of
// calendar days rather than a duration.
//
// ref may be zero, which means now -- the convention GetWeeklyConsumablesReport
// already uses.
func WeekOf(ref time.Time) *Week {
	if ref.IsZero() {
		ref = time.Now()
	}
	start, end := WeekRange(ref)

	// One source for "today", the same one the LCU gate and the maintenance due
	// dates use, so the highlighted column cannot disagree with what the rest of
	// the server thinks today is.
	today := Today().Format(time.DateOnly)

	w := &Week{
		Start: JSONDate(start),
		// WeekRange's end is the following Monday; the week the reader is looking
		// at closes on the Sunday before it.
		End:  JSONDate(end.AddDate(0, 0, -1)),
		Days: make([]WeekDay, 0, 7),
	}
	for i := 0; i < 7; i++ {
		// Already midnight, in ref's own location, so Format cannot land on the
		// wrong day for a reader west of UTC.
		d := start.AddDate(0, 0, i)
		w.Days = append(w.Days, WeekDay{
			Weekday: i + 1,
			Label:   d.Format("Mon"),
			Date:    JSONDate(d),
			IsToday: d.Format(time.DateOnly) == today,
		})
	}
	return w
}

// Shift is one reusable pattern: a name and the hours it covers.
type Shift struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Name      string    `json:"name"`

	// "HH:MM", 24-hour: exactly what <input type="time"> produces, and stored as
	// text for the reason issues.start_time is (migration 000007).
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`

	// Overnight is true when the shift finishes the following morning. Derived in
	// the scanner and never stored, so a flag cannot disagree with the two times it
	// is about.
	Overnight bool `json:"overnight"`

	// Color is an optional #rrggbb tint: "" means unset, which is the normal
	// state and renders exactly as this shift did before the column existed.
	// A string rather than a pointer because there is no third state to tell
	// apart -- NormaliseShiftColor makes "unset" and "empty" the same value, so a
	// pointer would buy nothing and cost a sql.NullString in the scanner and a
	// nil branch in every reader.
	//
	// Never omitempty: "color": "" is the wire contract that lets a client tell
	// "no tint" from "a server too old to have heard of tints".
	Color  string `json:"color"`
	Active bool   `json:"active"`

	// AssignmentCount is how many cells across the whole team use this shift. Read
	// alongside the row so the delete prompt can say what deleting it would cost
	// rather than quoting a count nobody kept -- the reason maintenance_schedules
	// carries its check count in its select list.
	AssignmentCount int `json:"assignment_count"`

	CreatedBy *int64 `json:"created_by"`
	UpdatedBy *int64 `json:"updated_by"`
	Version   int    `json:"version"`
}

// IsOvernight reports whether the shift runs past midnight.
//
// The rule is "the end is earlier in the day than the start, and is not midnight
// itself". The midnight clause is the whole subtlety: 16:00-00:00 is an eight-hour
// shift that finishes exactly at midnight, not an overnight one, and a bare
// `end < start` gets it wrong -- 00:00 is earlier than 16:00.
//
// Computed by parsing rather than by comparing strings. Lexicographically
// "22:00" > "06:00" happens to give the right answer for zero-padded 24-hour
// times, and the wrong answer the moment a value is not zero-padded -- which the
// CHECK refuses but a hand-edited row can still be. Unparseable input is not
// overnight; ValidateShift has already refused it by then.
func (s *Shift) IsOvernight() bool {
	sh, sm, ok1 := parseClockTime(s.StartTime)
	eh, em, ok2 := parseClockTime(s.EndTime)
	if !ok1 || !ok2 {
		return false
	}
	end := eh*60 + em
	return end > 0 && end < sh*60+sm
}

// DurationMinutes is how long the shift is, with an end earlier than the start read
// as the following morning: 22:00 to 06:00 is 480 minutes, not -960.
//
// Delegates to ComputeDurationMinutes, the same rule the issue form's start/end
// fields already use, so "how long was that shift" and "how long did that repair
// take" cannot drift apart. Zero when either time is unparseable or the two are
// equal, which ValidateShift has already refused.
func (s *Shift) DurationMinutes() int {
	minutes, _ := ComputeDurationMinutes(s.StartTime, s.EndTime)
	return minutes
}

// Label is how a shift reads in a cell: "08:00-16:00". The overnight case adds
// nothing, because a cell is small and "22:00-06:00" is how every rota in the
// world writes it; Overnight is there for code that needs to know, not for the
// label. A bare hyphen rather than an en dash, matching Room.Label and the rest of
// this codebase.
func (s *Shift) Label() string { return s.StartTime + "-" + s.EndTime }

// ValidateShift checks one shift definition: a name that can be typed into the
// cell picker, and two times that mean something.
//
// The overnight rule is the interesting one. An end earlier than the start is not
// an error, it is most of a 24-hour hotel's rota, so the only ordering refusal is
// the two being equal -- a shift nobody would ever be on, which renders as a cell
// that is simultaneously assigned and empty.
//
// Trims in place, like ValidateMaintenanceSchedule and ValidateUser, so what gets
// validated is what gets stored.
func ValidateShift(v *validator.Validator, s *Shift) {
	s.Name = strings.TrimSpace(s.Name)
	s.StartTime = strings.TrimSpace(s.StartTime)
	s.EndTime = strings.TrimSpace(s.EndTime)

	// Folded here rather than in the handler, so the stored value is canonical
	// whichever of the three writers -- create, update, retire -- produced it, and
	// so the Settings list cannot show two visually identical tints as different
	// colours. Placed before the name check rather than after the time checks'
	// early return: a bad tint and a bad name are independent complaints and
	// neither should mask the other.
	color, colorOK := NormaliseShiftColor(s.Color)
	s.Color = color
	v.Check(colorOK, "color", "must be a hex color in #RRGGBB format")

	v.Check(s.Name != "", "name", "must be provided")
	v.Check(len(s.Name) <= MaxShiftNameLength, "name",
		fmt.Sprintf("must not be more than %d characters", MaxShiftNameLength))
	v.Check(clockTimePattern.MatchString(s.StartTime), "start_time",
		"must be a time in HH:MM 24-hour format")
	v.Check(clockTimePattern.MatchString(s.EndTime), "end_time",
		"must be a time in HH:MM 24-hour format")

	// Only once both times are well formed is there a length to reason about.
	// Guarded so a malformed time does not also collect "must not be the same
	// time", which would be a second complaint about the same keystroke.
	if !clockTimePattern.MatchString(s.StartTime) || !clockTimePattern.MatchString(s.EndTime) {
		return
	}
	if minutes, ok := ComputeDurationMinutes(s.StartTime, s.EndTime); ok {
		v.Check(minutes <= MaxShiftMinutes, "end_time",
			fmt.Sprintf("must not be more than %d hours after the start time", MaxShiftMinutes/60))
		return
	}
	// Both parse but the length came out zero: the same clock time twice.
	v.AddError("end_time", "must not be the same time as the start time")
}

// NormaliseShiftColor turns whatever a client sent into either the canonical
// lower-case "#rrggbb" or "" for "no tint", and reports whether it was one of
// those.
//
// Three rules, each for a reason. Surrounding whitespace goes because
// ValidateShift trims every other field in place and a colour pasted out of a
// stylesheet arrives with a trailing space often enough to be worth a trim
// rather than a 422. The leading '#' is optional in and mandatory out, so a
// reader never has to know whether it is there. Case folds to LOWER, which is
// what <input type="color"> hands back, so the round trip stores the string the
// browser already produced instead of reformatting it.
//
// "" means unset, not invalid. Clearing the tint in the editor sends "", and that
// has to be a legal outcome distinct from a malformed value -- the same reason
// DayAssignment.ShiftID is a pointer.
//
// Reuses hexColorPattern from issue_types.go rather than declaring a second
// regexp for the same thing, which is also what makes the database CHECK in
// migration 000030 and this function the same expression.
func NormaliseShiftColor(raw string) (string, bool) {
	c := strings.TrimSpace(raw)
	if c == "" {
		return "", true
	}
	if !strings.HasPrefix(c, "#") {
		c = "#" + c
	}
	c = strings.ToLower(c)
	if !hexColorPattern.MatchString(c) {
		// Empty even on failure, so the field is always canonical-or-blank and no
		// future caller can reach the INSERT having skipped the check.
		return "", false
	}
	return c, true
}

// colorArg lifts a normalised tint to the value the column stores: nil for "no
// tint", so an unset tint is absent rather than an empty string that the CHECK
// would then also have to allow.
func colorArg(c string) any {
	if c == "" {
		return nil
	}
	return c
}

// shiftColumns is every field of a shift in one string, so the scan order below
// cannot drift from the select list. The assignment count is here for every read
// rather than only for the list, because maintenance_schedules already reads its
// check count on the single-row Get too, and at a dozen shifts over a few dozen
// cells the correlated count costs nothing.
const shiftColumns = `
	s.id, s.created_at, s.updated_at, s.name, s.start_time, s.end_time, s.color,
	s.active, s.created_by, s.updated_by, s.version,
	(SELECT COUNT(*) FROM shift_assignments a WHERE a.shift_id = s.id)`

// scanShift reads one shifts row in the order of shiftColumns.
//
// Takes a scanner as a plain func rather than an interface, because it is called
// with both a rows.Scan method value and a row.Scan one, and only a func parameter
// accepts the two -- the reason scanUser and scanMaintenanceSchedule are written
// this way.
//
// lead holds destinations for columns that come *before* the shift's own -- the
// user_id and weekday that ScheduleModel.Week prepends to the select list. It is
// variadic, and that is not tidiness: sql.Rows.Scan advances a cursor per call, so
// reading the two leading columns and calling this separately would read the
// shift's twelve columns from the *following row*. Every destination for one row
// has to go into one call.
func scanShift(scan func(dest ...any) error, lead ...any) (*Shift, error) {
	var (
		s                    Shift
		tint                 sql.NullString
		createdBy, updatedBy sql.NullInt64
		count                int64
	)
	dest := make([]any, 0, len(lead)+12)
	dest = append(dest, lead...)
	dest = append(dest,
		&s.ID, &s.CreatedAt, &s.UpdatedAt, &s.Name, &s.StartTime,
		&s.EndTime, &tint, &s.Active, &createdBy, &updatedBy, &s.Version, &count)
	if err := scan(dest...); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		s.CreatedBy = &createdBy.Int64
	}
	if updatedBy.Valid {
		s.UpdatedBy = &updatedBy.Int64
	}
	// A NULL tint leaves Color "", which is the "no tint" case every reader
	// already handles, so an untinted shift is indistinguishable from one that
	// predates migration 000030 -- which is exactly what it is.
	if tint.Valid {
		s.Color = tint.String
	}
	s.AssignmentCount = int(count)
	// Derived here so the flag and the two times it is about can never be stored
	// disagreeing, however the row got there.
	s.Overnight = s.IsOvernight()
	return &s, nil
}

// isDuplicateShift recognises the unique-constraint violation on shifts.name.
//
// Matched on the SQLSTATE rather than the message text: that is the newer of the
// two patterns in this package (consumable_items.go) and it does not break if the
// constraint is ever given an explicit name.
func isDuplicateShift(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

// ShiftModel is the rota's catalog of definitions: which shifts exist, and what
// hours they cover. Deliberately separate from ScheduleModel -- they are edited by
// different screens (Settings vs the Schedule page) at very different rates.
type ShiftModel struct {
	DB *sql.DB
}

// List returns the shifts in the order a rota is read in: active before retired,
// then earliest start time first, then id so two shifts starting together cannot
// swap places between requests.
//
// start_time rather than name because the order a manager thinks about shifts in
// is the order they happen. Pass includeRetired for the Settings list; the grid's
// legend and the cell picker want only the active ones.
//
// Not paged. This is a fixed, short list -- one row per shift pattern, of which a
// hotel has a handful -- and the picker wants all of it at once. It is the
// opposite case to the issue tables, which grow without bound.
func (m ShiftModel) List(includeRetired bool) ([]*Shift, error) {
	where := ""
	if !includeRetired {
		where = "WHERE s.active"
	}
	query := `SELECT ` + shiftColumns + `
		FROM shifts s ` + where + `
		ORDER BY s.active DESC, s.start_time ASC, s.id ASC`

	ctx, cancel := listContext()
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*Shift{}
	for rows.Next() {
		s, err := scanShift(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Get returns one shift, or ErrRecordNotFound.
func (m ShiftModel) Get(id int64) (*Shift, error) {
	query := `SELECT ` + shiftColumns + ` FROM shifts s WHERE s.id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	s, err := scanShift(m.DB.QueryRowContext(ctx, query, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	return s, err
}

// Create stores a new shift.
//
// updated_by starts as the creator, matching how maintenance_schedules is written
// on insert -- the two columns only diverge once somebody else edits it.
func (m ShiftModel) Create(s *Shift) error {
	query := `
		INSERT INTO shifts (name, start_time, end_time, color, active, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		RETURNING id, created_at, updated_at, version`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query,
		s.Name, s.StartTime, s.EndTime, colorArg(s.Color), s.Active, s.CreatedBy).
		Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt, &s.Version)
	if err != nil {
		if isDuplicateShift(err) {
			return ErrDuplicateShift
		}
		return err
	}
	s.Overnight = s.IsOvernight()
	return nil
}

// Update changes a shift's definition, guarded by the version it was read at.
//
// ErrEditConflict rather than ErrRecordNotFound when the version no longer matches:
// the row is still there, somebody else simply saved first, and the manager needs
// to be told to reload rather than that the shift is gone. UserModel.Update makes
// the same distinction.
func (m ShiftModel) Update(s *Shift) error {
	query := `
		UPDATE shifts
		SET name=$1, start_time=$2, end_time=$3, color=$4, active=$5,
		    updated_at=NOW(), updated_by=$6, version=version+1
		WHERE id=$7 AND version=$8
		RETURNING updated_at, version`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query,
		s.Name, s.StartTime, s.EndTime, colorArg(s.Color), s.Active, s.UpdatedBy, s.ID, s.Version).
		Scan(&s.UpdatedAt, &s.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrEditConflict
	}
	if err != nil {
		if isDuplicateShift(err) {
			return ErrDuplicateShift
		}
		return err
	}
	s.Overnight = s.IsOvernight()
	return nil
}

// Delete removes a shift, and by cascade the cells that pointed at it.
//
// Assignments do NOT make the delete fail. A rota cell pointing at a shift nobody
// has defined any more has nothing to say, and refusing the delete would mean a
// manager cannot correct a mistyped shift name without first clearing every cell
// that references it. The migration comment sets out why CASCADE is right and why
// RESTRICT (the choice maintenance_schedules.department_id makes) does not apply
// here. Shift.AssignmentCount is there so the caller can warn before the button is
// pressed rather than after.
//
// ErrRecordNotFound when there was no such shift.
func (m ShiftModel) Delete(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, `DELETE FROM shifts WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return ErrRecordNotFound
	}
	return nil
}

// ScheduleMember is one row of the grid: a person, and what they are on for each of
// the seven days.
type ScheduleMember struct {
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	AvatarIdx int    `json:"avatar_idx"`
	Role      string `json:"role"`

	// Active is false for a deactivated account. Such a member is still a row, with
	// their week still stored, until a manager clears it -- see Week for why
	// dropping the row would be worse.
	Active bool `json:"active"`

	// Days is indexed by ISO weekday minus one: Days[0] is Monday, Days[6] is
	// Sunday, matching Week.Days position for position so the grid is a zip.
	//
	// A day nobody is assigned is a nil entry and marshals as null. That is the
	// entire reason the array is fixed-length: there is no state in which the
	// client has to guess whether a missing cell means "off" or "the server
	// forgot", and no way for a zero-valued Shift -- id 0, name "", times 00:00 --
	// to appear as a real shift. It is also what makes the empty grid safe: with no
	// shifts configured at all, every row is seven nulls and nothing else has to
	// change.
	Days [7]*Shift `json:"days"`
}

// ScheduleWeek is the rota as the grid consumes it, and the dated exceptions to it.
//
// Absences is a flat list of RANGES rather than something per member, for the
// reason AbsenceRef gives: a per-member per-day array cannot say whether a run
// ends inside the week on screen or carries on past it. A flat list also keeps
// Days untouched, which matters because Days is the recurring pattern and
// everything about how this feature reads it is built on that staying put.
//
// Only absences touching the week on screen appear, so a year of holidays is not
// shipped to a noticeboard that shows seven days.
type ScheduleWeek struct {
	Week     *Week             `json:"week"`
	Members  []*ScheduleMember `json:"members"`
	Absences []*AbsenceRef     `json:"absences"`
}

// ScheduleModel is the recurring pattern itself: who is on what, on which weekday.
// It knows nothing about any particular week -- that is the point.
type ScheduleModel struct {
	DB *sql.DB
}

// Week returns the rota for the week containing ref.
//
// THREE queries, all constant in the number of members, and that is the whole point.
// Reading per member would be members × 7 queries, which is the N+1 shape this
// codebase has already had to unpick twice (loadIssueChildren,
// LCUModel.testsForUnits).
//
//  1. every user, with the five fields a row needs and nothing else;
//  2. every assignment, joined to its shift;
//  3. every absence touching the week on screen.
//
// ONLY THE THIRD IS DATE-FILTERED, and the difference is the entire design of this
// feature rather than an inconsistency. Query 2 has no WHERE clause, and must not
// grow one: the rota is the recurring pattern, so ALL of the assignments ARE what
// the grid shows. There is no per-date slice to narrow it to, and a filter there
// is the "filter by week" mistake that makes the rota come back with seven cells
// for one week and none for another. The whole table is bounded by members × 7 -- a
// few dozen rows -- so it is a range scan on the left of the primary key.
//
// Query 3 is the first genuinely date-ranged read in this function, and it is
// bounded by exactly the seven dates the grid prints. It filters the EXCEPTIONS to
// the pattern; it does not narrow the pattern. If you are reading this and
// wondering whether to add a week filter to query 2: that is the mistake, and the
// symptom is an empty grid rather than an error.
//
// Deactivated members are included, ordered last. Accounts are deactivated rather
// than deleted (deactivateUserHandler only flips active=false and revokes tokens),
// so dropping the row would make a person's stored week invisible while still being
// present -- and a later reactivation would resurrect a row the manager thought they
// had tidied away. Keeping the row makes "clear this person's week" a deliberate
// act, and Active is in the payload so the page can render them muted.
//
// The users query deliberately does not reuse scanUser. That scanner selects
// password_hash and its struct carries the account's email address, and this is
// served to callers with no token at all. The columns are written out here so that
// adding one to users in six months cannot make it appear on a public noticeboard.
func (m ScheduleModel) Week(ref time.Time) (*ScheduleWeek, error) {
	ctx, cancel := listContext()
	defer cancel()

	out := &ScheduleWeek{
		Week:     WeekOf(ref),
		Members:  []*ScheduleMember{},
		Absences: []*AbsenceRef{},
	}

	rows, err := m.DB.QueryContext(ctx, `
		SELECT id, name, avatar_idx, role, active
		FROM users
		ORDER BY active DESC, created_at ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	byUser := make(map[int64]*ScheduleMember, 16)
	for rows.Next() {
		var m ScheduleMember
		if err := rows.Scan(&m.UserID, &m.Name, &m.AvatarIdx, &m.Role, &m.Active); err != nil {
			rows.Close()
			return nil, err
		}
		out.Members = append(out.Members, &m)
		byUser[m.UserID] = &m
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Everything else is one read, not one per member.
	//
	// The outer table is aliased sa, not a: shiftColumns already uses `a` for its
	// correlated count, and shadowing it would be a silent trap rather than an
	// error.
	shiftRows, err := m.DB.QueryContext(ctx, `
		SELECT sa.user_id, sa.weekday, `+shiftColumns+`
		FROM shift_assignments sa
		INNER JOIN shifts s ON s.id = sa.shift_id
		ORDER BY sa.user_id, sa.weekday`)
	if err != nil {
		return nil, err
	}
	// Deferred as the safety net for the early returns inside the loop below, and
	// closed explicitly after it as well -- an open sql.Rows holds its connection,
	// so deferring alone would issue the absences query at the bottom while this
	// cursor is still checked out. Invisible at the default 25 connections, and a
	// deadlock at -db-max-open-conns=1.
	defer shiftRows.Close()

	for shiftRows.Next() {
		var (
			userID  int64
			weekday int
		)
		// One Scan for all fifteen columns -- see scanShift on why the two leading
		// ones cannot be read separately.
		shift, err := scanShift(shiftRows.Scan, &userID, &weekday)
		if err != nil {
			return nil, err
		}
		member, found := byUser[userID]
		if !found {
			// Cannot happen while the FK holds -- the first query reads every user
			// without a filter -- but a cell landing nowhere would be a silent drop,
			// and silent drops are what the no-filter decision above is most
			// vulnerable to.
			continue
		}
		// The CHECK keeps weekday in 1..7; the belt is the array bound.
		if weekday < 1 || weekday > 7 {
			continue
		}
		member.Days[weekday-1] = shift
	}
	if err := shiftRows.Err(); err != nil {
		shiftRows.Close()
		return nil, err
	}
	if err := shiftRows.Close(); err != nil {
		return nil, err
	}

	// The dated exceptions to everything above, in one read rather than one per
	// member. Bounded by [week start, week end] BOTH inclusive, which are already
	// computed as the grid's own first and last day -- so the absences and the seven
	// columns can never be filtered by different weeks, which would show a holiday
	// on the wrong day and is the kind of disagreement that is invisible until
	// somebody is off sick on a day the board says they are working.
	absences, err := AbsenceModel{DB: m.DB}.InWindow(ctx,
		out.Week.Start.Time(), out.Week.End.Time())
	if err != nil {
		return nil, err
	}
	for _, a := range absences {
		out.Absences = append(out.Absences, a.Ref())
	}

	return out, nil
}

// DayAssignment is one cell as sent by a manager: this weekday, and either a shift
// or nothing.
//
// ShiftID is a pointer because nil is not "unchanged" here, it is "off" -- a real
// outcome with its own row deletion. A plain int64 could not tell those apart,
// which is the same reason Settings.SLAMinutes is a pointer and
// MaintenanceSchedule.AssigneeID is too.
type DayAssignment struct {
	Weekday int    `json:"weekday"`
	ShiftID *int64 `json:"shift_id"`
}

// ValidateDayAssignments checks a submitted week before anything is written.
//
// All errors land on the single "days" key, because the client sends them as one
// list rather than as a form of seven fields. validator.AddError keeps only the
// first per key, so the message has to stand on its own -- hence naming the
// offending weekday in it.
func ValidateDayAssignments(v *validator.Validator, days []DayAssignment) {
	v.Check(len(days) <= MaxAssignmentsPerRequest, "days",
		fmt.Sprintf("must not contain more than %d days", MaxAssignmentsPerRequest))

	seen := make(map[int]bool, len(days))
	for _, d := range days {
		v.Check(d.Weekday >= 1 && d.Weekday <= 7, "days",
			fmt.Sprintf("weekday %d is not a day of the week (1 is Monday, 7 is Sunday)", d.Weekday))
		if seen[d.Weekday] {
			v.AddError("days", fmt.Sprintf("weekday %d is listed more than once", d.Weekday))
		}
		seen[d.Weekday] = true
	}
}

// SetWeek makes one member's week match what was sent: the days listed are written,
// and any day not listed has its assignment removed.
//
// One transaction for the whole row. A failure between two cells would leave the
// grid showing a manager a week they did not save, and half a rota is not a state
// anybody can act on. At most seven writes, so the loop is not a concern; this is
// the same shape as TVSwapModel.SetForIssue.
//
// No version guard, and deliberately. See the table comment: an assignment is only
// ever written by a PUT carrying the caller's whole row, so there is no
// read-modify-write cycle to interleave with.
func (m ScheduleModel) SetWeek(userID int64, days []DayAssignment, updatedBy *int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// The weekdays that end the request holding a shift. Everything else is a day
	// off by the time the DELETE runs.
	kept := make([]int, 0, len(days))
	for _, d := range days {
		if d.ShiftID == nil {
			continue
		}
		kept = append(kept, d.Weekday)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO shift_assignments (user_id, weekday, shift_id, updated_by)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id, weekday) DO UPDATE
			SET shift_id   = EXCLUDED.shift_id,
			    updated_at = NOW(),
			    updated_by = EXCLUDED.updated_by`,
			userID, d.Weekday, *d.ShiftID, updatedBy); err != nil {
			return err
		}
	}

	// Days the request did not mention are days off.
	//
	// kept must be non-nil: a nil slice binds as SQL NULL, and `weekday <> ALL(NULL)`
	// is NULL rather than true, so nothing would be deleted -- including the cells
	// just written. That is the trap SetForIssue calls out by name.
	//
	// An empty slice binds as '{}', and `weekday <> ALL('{}')` is true for every
	// row, which is exactly the "clear this person's whole week" case.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM shift_assignments
		WHERE user_id = $1 AND weekday <> ALL($2::smallint[])`,
		userID, pq.Array(kept)); err != nil {
		return err
	}

	return tx.Commit()
}
