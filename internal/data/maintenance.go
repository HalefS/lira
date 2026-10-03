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

// Preventive maintenance on department equipment.
//
// The rule the whole feature rests on: a check that found nothing does not
// become an issue. Most checks find nothing, and an issue row per check would
// dilute every number the app reports -- the issue count, the average
// resolution time, the recurring-alert detection that matches on issue history.
// So a check is its own record and an issue is something the technician chooses
// to raise when the check turned up a fault.

// Equipment classes that are maintained on a schedule. Fixed rather than a table
// because this is a short, stable list of what the hotel owns and there is no
// per-type configuration anywhere near it -- a checklist, colour or category
// would want a table, and none of that exists.
const (
	EquipmentPrinter = "printer"
	EquipmentPhone   = "phone"
)

// How a check turned out.
const (
	MaintenanceOK    = "ok"
	MaintenanceFault = "fault"
)

// Cadence bounds, in days.
//
// A week is the tightest sensible round: anything more often than that is a
// defect being reported, not equipment being maintained, and it would make the
// queue permanently red without telling anyone anything. Two years is the
// loosest -- a yearly service, with enough room to spell it in days.
const (
	MinMaintenanceIntervalDays = 7
	MaxMaintenanceIntervalDays = 730

	// Default per class, applied when a device is added without a cadence.
	// Printers are worked hard and jam, run hot and need their rollers and
	// consumables watched; a phone handset or extension board is inert and
	// faults far less often.
	DefaultPrinterIntervalDays = 30
	DefaultPhoneIntervalDays   = 90
)

const MaxMaintenanceDeviceNameLength = 80
const MaxMaintenanceNoteLength = 500

// DueSoonDays is how far ahead a schedule counts as coming up rather than
// merely scheduled. A fortnight is roughly the gap between noticing something
// needs doing and actually getting to it, so anything inside it is worth
// showing at the top of the queue rather than buried in a list of dates.
const DueSoonDays = 14

// Schedule states. Derived at read time from next_due_on against today's date,
// never stored: a schedule left alone overnight must become overdue on its own,
// which a stored flag would not do.
const (
	DueOverdue   = "overdue"
	DueSoon      = "due_soon"
	DueScheduled = "scheduled"
)

// MaintenanceModel is the preventive-maintenance store.
type MaintenanceModel struct {
	DB *sql.DB
}

// JSONDate is a calendar date with no time of day, which is what a Postgres
// `date` column is and what every other date-only field in this app already
// sends.
//
// Left as a bare time.Time it would serialise as 2026-11-02T00:00:00Z, and every
// reader would have to slice the string to get a day back -- which is the exact
// shape that comes out a day wrong for any reader east of UTC. The server's
// timezone is the authority for "when is a check due", so the date has to leave
// here as a plain date and mean the same day everywhere.
type JSONDate time.Time

func (d JSONDate) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Time(d).Format(time.DateOnly) + `"`), nil
}

// Time returns the underlying time for the date arithmetic, which is written
// against plain time.Time throughout.
func (d JSONDate) Time() time.Time { return time.Time(d) }

// ptrOf lifts a JSONDate out to the *time.Time the date maths is written against.
func ptrOf(d JSONDate) *time.Time {
	t := d.Time()
	return &t
}

// datePtr is nil for a date column that is SQL NULL, which is how a device that
// has never been checked is distinguished from one checked on the epoch.
func datePtr(t *time.Time) *JSONDate {
	if t == nil {
		return nil
	}
	d := JSONDate(*t)
	return &d
}

// MaintenanceSchedule is one device to keep an eye on.
type MaintenanceSchedule struct {
	ID            int64  `json:"id"`
	DeviceName    string `json:"device_name"`
	EquipmentType string `json:"equipment_type"`
	DepartmentID  int64  `json:"department_id"`
	// Joined in rather than stored, so renaming a department shows up
	// everywhere instead of leaving history pointing at a name that is gone.
	DepartmentName string `json:"department_name"`
	IntervalDays   int    `json:"interval_days"`
	Active         bool   `json:"active"`
	// Who the check belongs to. Nil means everybody: any technician may take it.
	// Never enforced on writes -- only on who is shown the row -- because the
	// people doing the work are the technicians, and a device that quietly became
	// unrecordable would simply be skipped.
	AssigneeID   *int64 `json:"assignee_id"`
	AssigneeName string `json:"assignee_name"`

	LastDoneOn *JSONDate `json:"last_done_on"`
	// Null until the device has been checked once. The zero outcome is not the
	// same as a check that found nothing.
	LastOutcome *string `json:"last_outcome"`
	LastNote    string  `json:"last_note"`

	NextDueOn JSONDate `json:"next_due_on"`
	// How many checks this device has on record. Read alongside the row so the
	// delete prompt can say what deleting it would cost, rather than quoting a
	// count nobody kept.
	CheckCount int    `json:"check_count"`
	DueState   string `json:"due_state"`
	// Whole days past due. Zero when not overdue, and negative never: the number
	// is "how late", so a schedule in the future reports zero rather than a
	// negative age.
	DaysOverdue int `json:"days_overdue"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy *int64    `json:"created_by"`
	UpdatedBy *int64    `json:"updated_by"`
	Version   int       `json:"version"`
}

// MaintenanceCheck is one occasion the device was actually looked at.
type MaintenanceCheck struct {
	ID         int64    `json:"id"`
	ScheduleID int64    `json:"schedule_id"`
	DoneOn     JSONDate `json:"done_on"`
	Outcome    string   `json:"outcome"`
	Note       string   `json:"note"`
	DoneBy     int64    `json:"done_by"`
	DoneByName string   `json:"done_by_name"`
	// The fault this check turned up, when one was raised. Null for a clean
	// check, and for a fault dealt with on the spot.
	IssueID    *int64    `json:"issue_id"`
	IssueLabel string    `json:"issue_label"`
	CreatedAt  time.Time `json:"created_at"`
}

// DefaultIntervalDays is the cadence for a class when none was given.
func DefaultIntervalDays(equipmentType string) int {
	if equipmentType == EquipmentPhone {
		return DefaultPhoneIntervalDays
	}
	return DefaultPrinterIntervalDays
}

// ClampMaintenanceIntervalDays keeps a cadence inside the bounds, so a bad value
// from a settings screen or a hand-edited row cannot produce a schedule that is
// permanently overdue or that never comes round again.
func ClampMaintenanceIntervalDays(days int) int {
	if days < MinMaintenanceIntervalDays {
		return MinMaintenanceIntervalDays
	}
	if days > MaxMaintenanceIntervalDays {
		return MaxMaintenanceIntervalDays
	}
	return days
}

// MaintenanceDueOn is the next date a device wants looking at.
//
// Counted from the last time the work was actually done, never from a calendar
// grid and never from when it was last marked done in advance. That is what stops
// the queue being satisfied by ticking boxes: there is no "catch up" button,
// because a device that has not been looked at is still due.
func MaintenanceDueOn(lastDoneOn *time.Time, createdOn time.Time, intervalDays int) time.Time {
	base := createdOn
	if lastDoneOn != nil {
		base = *lastDoneOn
	}
	return time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, ClampMaintenanceIntervalDays(intervalDays))
}

// MaintenanceDueState classifies a schedule against today.
func MaintenanceDueState(nextDueOn time.Time, today time.Time, active bool) string {
	if !active {
		return DueScheduled
	}
	due := time.Date(nextDueOn.Year(), nextDueOn.Month(), nextDueOn.Day(), 0, 0, 0, 0, time.UTC)
	now := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	switch days := int(now.Sub(due).Hours() / 24); {
	case days > 0:
		return DueOverdue
	case days >= -DueSoonDays:
		return DueSoon
	default:
		return DueScheduled
	}
}

func ValidateMaintenanceSchedule(v *validator.Validator, s *MaintenanceSchedule) {
	s.DeviceName = strings.TrimSpace(s.DeviceName)
	v.Check(s.DeviceName != "", "device_name", "must be provided")
	v.Check(len(s.DeviceName) <= MaxMaintenanceDeviceNameLength, "device_name",
		fmt.Sprintf("must not be more than %d characters", MaxMaintenanceDeviceNameLength))
	v.Check(s.EquipmentType == EquipmentPrinter || s.EquipmentType == EquipmentPhone,
		"equipment_type", "must be 'printer' or 'phone'")
	v.Check(s.DepartmentID > 0, "department_id", "must be provided")
	v.Check(s.IntervalDays >= MinMaintenanceIntervalDays, "interval_days",
		fmt.Sprintf("must be at least %d days", MinMaintenanceIntervalDays))
	v.Check(s.IntervalDays <= MaxMaintenanceIntervalDays, "interval_days",
		fmt.Sprintf("must not be more than %d days", MaxMaintenanceIntervalDays))
}

func ValidateMaintenanceCheck(v *validator.Validator, c *MaintenanceCheck) {
	c.Note = strings.TrimSpace(c.Note)
	v.Check(c.Outcome == MaintenanceOK || c.Outcome == MaintenanceFault, "outcome",
		"must be 'ok' or 'fault'")
	v.Check(len(c.Note) <= MaxMaintenanceNoteLength, "note",
		fmt.Sprintf("must not be more than %d characters", MaxMaintenanceNoteLength))
}

// listContext, shared with the rest of the listings.
func maintenanceListContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

const maintenanceScheduleColumns = `
	ms.id, ms.device_name, ms.equipment_type, ms.department_id,
	ms.interval_days, ms.active,
	ms.last_done_on, ms.last_outcome, ms.last_note,
	ms.next_due_on, ms.created_at, ms.updated_at,
	ms.created_by, ms.updated_by, ms.version,
	ms.assignee_id,
	(SELECT COUNT(*) FROM maintenance_checks mc WHERE mc.schedule_id = ms.id)`

// ListSchedules returns the devices a given user is responsible for, joined to
// their department and classified against today.
//
// Not paged: this is a fixed, short list -- one row per physical device, of which
// a hotel has tens -- and the queue wants all of it at once to sort into due /
// due soon / scheduled. It is the opposite case to the
// issue tables, which grow without bound.
// MaintenanceScope narrows a listing to one user's responsibility.
//
// A manager passes isManager and gets everything, because they administer the
// schedule rather than work through it. A technician gets the checks that are
// theirs plus the ones nobody has claimed.
//
// argIndex is the placeholder the caller has free for the user id, because these
// queries all bind something else first -- a limit, a due date. Passing it in is
// clearer than rewriting the placeholders afterwards, which is the kind of
// cleverness that breaks silently the day a query gains an argument.
func MaintenanceScope(userID int64, isManager bool, argIndex int) (sql string, args []any) {
	if isManager {
		return "", nil
	}
	return fmt.Sprintf(`(ms.assignee_id IS NULL OR ms.assignee_id = $%d)`, argIndex), []any{userID}
}

func (m MaintenanceModel) ListSchedules(today time.Time, scopeUserID int64, isManager bool) ([]*MaintenanceSchedule, error) {
	conds := []string{}
	var args []any
	// The active set only, and only for a technician. A manager's page has a
	// Retired section as well, and filtering those out here would leave it
	// permanently empty -- retired devices are a manager's to look at, their
	// history and all. A technician has no work attached to a retired device, so
	// showing them one would just be noise in their queue.
	if !isManager {
		conds = append(conds, "ms.active")
	}
	if scope, sargs := MaintenanceScope(scopeUserID, isManager, 1); scope != "" {
		conds = append(conds, scope)
		args = sargs
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	query := `SELECT` + maintenanceScheduleColumns + `, d.name, COALESCE(a.name, '')
		FROM maintenance_schedules ms
		INNER JOIN departments d ON d.id = ms.department_id
		LEFT JOIN users a ON a.id = ms.assignee_id
		` + where + `
		ORDER BY ms.active DESC, ms.next_due_on ASC, d.name ASC, ms.device_name ASC`

	ctx, cancel := maintenanceListContext()
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*MaintenanceSchedule{}
	for rows.Next() {
		s, err := scanMaintenanceSchedule(rows.Scan)
		if err != nil {
			return nil, err
		}
		s.DueState = MaintenanceDueState(s.NextDueOn.Time(), today, s.Active)
		s.DaysOverdue = maintenanceDaysOverdue(s.NextDueOn.Time(), today)
		out = append(out, s)
	}
	return out, rows.Err()
}

// maintenanceDaysOverdue reports how many whole days late a schedule is, and zero
// for anything not yet due. Whole days because "2 days overdue" is the useful
// thing to say and "1.6 days overdue" is not.
func maintenanceDaysOverdue(nextDueOn, today time.Time) int {
	due := time.Date(nextDueOn.Year(), nextDueOn.Month(), nextDueOn.Day(), 0, 0, 0, 0, time.UTC)
	now := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	if !due.Before(now) {
		return 0
	}
	return int(now.Sub(due).Hours() / 24)
}

// Takes the scanner as a plain function rather than an interface, because it is
// called with both a rows.Scan method value and a row.Scan one, and only a func
// parameter accepts the two.
func scanMaintenanceSchedule(scan func(dest ...any) error) (*MaintenanceSchedule, error) {
	var s MaintenanceSchedule
	var lastDone sql.NullTime
	var lastOutcome sql.NullString
	var createdBy, updatedBy, assigneeID sql.NullInt64
	var nextDue time.Time
	if err := scan(
		&s.ID, &s.DeviceName, &s.EquipmentType, &s.DepartmentID,
		&s.IntervalDays, &s.Active,
		&lastDone, &lastOutcome, &s.LastNote,
		&nextDue, &s.CreatedAt, &s.UpdatedAt,
		&createdBy, &updatedBy, &s.Version, &s.AssigneeID, &s.CheckCount,
		// The two joined names, last, because every query that selects them puts
		// them there last -- forgetting one is a scan arity error at run time
		// rather than a compile error, so both are named.
		&s.DepartmentName, &s.AssigneeName,
	); err != nil {
		return nil, err
	}
	if lastDone.Valid {
		s.LastDoneOn = datePtr(&lastDone.Time)
	}
	s.NextDueOn = JSONDate(nextDue)
	if lastOutcome.Valid {
		v := lastOutcome.String
		s.LastOutcome = &v
	}
	if assigneeID.Valid {
		id := assigneeID.Int64
		s.AssigneeID = &id
	}
	if createdBy.Valid {
		id := createdBy.Int64
		s.CreatedBy = &id
	}
	if updatedBy.Valid {
		id := updatedBy.Int64
		s.UpdatedBy = &id
	}
	return &s, nil
}

// GetSchedule returns one device, or ErrRecordNotFound.
func (m MaintenanceModel) GetSchedule(id int64) (*MaintenanceSchedule, error) {
	query := `SELECT` + maintenanceScheduleColumns + `, d.name, COALESCE(a.name, '')
		FROM maintenance_schedules ms
		INNER JOIN departments d ON d.id = ms.department_id
		LEFT JOIN users a ON a.id = ms.assignee_id
		WHERE ms.id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	row := m.DB.QueryRowContext(ctx, query, id)
	s, err := scanMaintenanceSchedule(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	return s, err
}

func (m MaintenanceModel) InsertSchedule(s *MaintenanceSchedule) error {
	// next_due_on is computed here, from the one rule in MaintenanceDueOn, rather
	// than by the caller, so a schedule can never be inserted already overdue for
	// no reason or with a due date that disagrees with its interval.
	next := MaintenanceDueOn(nil, time.Now(), s.IntervalDays)

	query := `
		INSERT INTO maintenance_schedules
			(device_name, equipment_type, department_id, interval_days, active,
			 next_due_on, assignee_id, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
		RETURNING id, created_at, updated_at, version`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query,
		s.DeviceName, s.EquipmentType, s.DepartmentID, s.IntervalDays, s.Active,
		next, s.AssigneeID, s.CreatedBy,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt, &s.Version)
	if err != nil {
		return err
	}
	s.NextDueOn = JSONDate(next)
	return nil
}

// UpdateSchedule edits a device's details or cadence.
//
// Changing the interval re-derives the due date from the last check rather than
// from today, so a device checked a week ago against a monthly round does not
// suddenly gain six days the moment a manager widens its interval. A device
// never checked is re-derived from when it was added.
func (m MaintenanceModel) UpdateSchedule(s *MaintenanceSchedule) error {
	existing, err := m.GetSchedule(s.ID)
	if err != nil {
		return err
	}

	base := existing.CreatedAt
	var lastDone *time.Time
	if existing.LastDoneOn != nil {
		t := existing.LastDoneOn.Time()
		lastDone = &t
		base = t
	}
	next := MaintenanceDueOn(lastDone, base, s.IntervalDays)

	// version guards against two managers saving over each other.
	query := `
		UPDATE maintenance_schedules
		SET device_name=$1, equipment_type=$2, department_id=$3,
		    interval_days=$4, active=$5, next_due_on=$6,
		    assignee_id=$7,
		    updated_at=NOW(), updated_by=$8, version=version+1
		WHERE id=$9 AND version=$10
		RETURNING updated_at, version`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = m.DB.QueryRowContext(ctx, query,
		s.DeviceName, s.EquipmentType, s.DepartmentID, s.IntervalDays, s.Active,
		next, s.AssigneeID, s.UpdatedBy, s.ID, s.Version,
	).Scan(&s.UpdatedAt, &s.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRecordNotFound
	}
	if err != nil {
		return err
	}
	s.NextDueOn = JSONDate(next)
	return nil
}

// DeleteSchedule removes a device and, by cascade, its whole check history.
//
// This is a hard delete and the API layer is responsible for saying so plainly,
// because "retire this device" and "delete the record that we serviced it
// eleven times" are not the same request. Retiring is `active=false`, which
// keeps the history and takes the device out of the queue.
func (m MaintenanceModel) DeleteSchedule(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, `DELETE FROM maintenance_schedules WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); rows == 0 && err == nil {
		return ErrRecordNotFound
	}
	return nil
}

// RecordCheck files one completed check and moves the schedule's due date.
//
// The check and the schedule's new state are written in one transaction. If they
// were separate, a failure between them would leave a check in the history that
// the schedule does not know about -- so the device would still read as due, be
// done again, and quietly accumulate duplicates.
func (m MaintenanceModel) RecordCheck(c *MaintenanceCheck) (*MaintenanceCheck, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Read the schedule inside the transaction so the interval used to derive the
	// next due date cannot have changed under us between the read and the write.
	var intervalDays int
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT interval_days, created_at
		FROM maintenance_schedules WHERE id=$1 FOR UPDATE`, c.ScheduleID).
		Scan(&intervalDays, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	if err != nil {
		return nil, err
	}

	query := `
		INSERT INTO maintenance_checks (schedule_id, done_on, outcome, note, done_by, issue_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`
	err = tx.QueryRowContext(ctx, query,
		c.ScheduleID, c.DoneOn.Time(), c.Outcome, c.Note, c.DoneBy, c.IssueID).
		Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		return nil, err
	}

	next := MaintenanceDueOn(ptrOf(c.DoneOn), createdAt, intervalDays)
	if _, err := tx.ExecContext(ctx, `
		UPDATE maintenance_schedules
		SET last_done_on=$1, last_outcome=$2, last_note=$3, next_due_on=$4,
		    updated_at=NOW(), updated_by=$5, version=version+1
		WHERE id=$6`,
		c.DoneOn.Time(), c.Outcome, c.Note, next, c.DoneBy, c.ScheduleID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return c, nil
}

// ListChecks returns one device's history, newest first, with the fault each
// check raised summarised alongside it.
func (m MaintenanceModel) ListChecks(scheduleID int64) ([]*MaintenanceCheck, error) {
	query := `
		SELECT c.id, c.schedule_id, c.done_on, c.outcome, c.note, c.done_by,
		       u.name, c.issue_id, c.created_at,
		       COALESCE(i.type || ' - ' || i.location, '')
		FROM maintenance_checks c
		INNER JOIN users u ON u.id = c.done_by
		LEFT JOIN issues i ON i.id = c.issue_id
		WHERE c.schedule_id = $1
		ORDER BY c.done_on DESC, c.id DESC`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*MaintenanceCheck{}
	for rows.Next() {
		var c MaintenanceCheck
		var doneOn time.Time
		var issueID sql.NullInt64
		if err := rows.Scan(
			&c.ID, &c.ScheduleID, &doneOn, &c.Outcome, &c.Note, &c.DoneBy,
			&c.DoneByName, &issueID, &c.CreatedAt, &c.IssueLabel,
		); err != nil {
			return nil, err
		}
		c.DoneOn = JSONDate(doneOn)
		if issueID.Valid {
			id := issueID.Int64
			c.IssueID = &id
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// ListAllChecks returns recent checks across the devices a user is responsible
// for, for the page's "recent activity" list.
//
// Scoped the same way as the queue: showing a technician the history of devices
// assigned to somebody else would tell them who is behind on their round, which
// is the same thing as showing them the devices.
func (m MaintenanceModel) ListAllChecks(limit int, scopeUserID int64, isManager bool) ([]*MaintenanceCheck, error) {
	// $1 is the limit, so the scope's user id is bound as $2.
	where, scopeArgs := MaintenanceScope(scopeUserID, isManager, 2)
	scope := ""
	if where != "" {
		scope = " AND " + where
	}

	query := `
		SELECT c.id, c.schedule_id, c.done_on, c.outcome, c.note, c.done_by,
		       u.name, c.issue_id, c.created_at,
		       COALESCE(i.type || ' - ' || i.location, '')
		FROM maintenance_checks c
		INNER JOIN users u ON u.id = c.done_by
		LEFT JOIN issues i ON i.id = c.issue_id
		INNER JOIN maintenance_schedules ms ON ms.id = c.schedule_id
		WHERE 1=1` + scope + `
		ORDER BY c.done_on DESC, c.id DESC
		LIMIT $1`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, append([]any{limit}, scopeArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*MaintenanceCheck{}
	for rows.Next() {
		var c MaintenanceCheck
		var doneOn time.Time
		var issueID sql.NullInt64
		if err := rows.Scan(
			&c.ID, &c.ScheduleID, &doneOn, &c.Outcome, &c.Note, &c.DoneBy,
			&c.DoneByName, &issueID, &c.CreatedAt, &c.IssueLabel,
		); err != nil {
			return nil, err
		}
		c.DoneOn = JSONDate(doneOn)
		if issueID.Valid {
			id := issueID.Int64
			c.IssueID = &id
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// CheckScheduleID returns the device a recorded check belongs to, so a caller
// holding only a check id can still decide whether this user may touch it. A
// check is reached by its own id in the link endpoint, so the visibility rule has
// to be able to cross back to the device it belongs to.
func (m MaintenanceModel) CheckScheduleID(checkID int64) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var scheduleID int64
	err := m.DB.QueryRowContext(ctx,
		`SELECT schedule_id FROM maintenance_checks WHERE id = $1`, checkID).Scan(&scheduleID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrRecordNotFound
	}
	return scheduleID, err
}

// LinkIssue points a recorded check at the issue it turned up.
//
// Separate from RecordCheck because the fault is found first and the issue is
// written afterwards: the technician opens the check, ticks "found a fault", and
// only then gets asked to log it. Recording the check first and patching the
// link on keeps that natural order without a two-phase write.
func (m MaintenanceModel) LinkIssue(checkID int64, issueID *int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx,
		`UPDATE maintenance_checks SET issue_id=$1 WHERE id=$2`, issueID, checkID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); rows == 0 && err == nil {
		return ErrRecordNotFound
	}
	return nil
}

// CountDue is how many active devices are overdue or coming up, for the nav
// badge.
//
// Counted over the database rather than off whatever the queue last returned, so
// the badge cannot disagree with the page. Scoped to the user for the same
// reason -- a badge counting work that is not on the page would be a number
// about somebody else's backlog.
func (m MaintenanceModel) CountDue(today time.Time, scopeUserID int64, isManager bool) (int, error) {
	soon := MaintenanceDueOn(&today, today, DueSoonDays)
	scope, args := MaintenanceScope(scopeUserID, isManager, 2)

	clause := ""
	if scope != "" {
		// $1 is the due-date bound, so the scope lands as $2 and its argument
		// goes second.
		args = append([]any{soon}, args...)
		clause = " AND " + scope
	} else {
		args = []any{soon}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var count int
	err := m.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM maintenance_schedules ms
		WHERE ms.active AND ms.next_due_on <= $1`+clause, args...).Scan(&count)
	return count, err
}
