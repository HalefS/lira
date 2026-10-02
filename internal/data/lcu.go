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

// The length of a reader's trial is a manager-tunable setting rather than a
// constant, so these three bound it: the default a fresh install starts on, the
// shortest window that is still a trial at all, and the longest that keeps the
// daily log and the prompt usable.
const (
	DefaultLCUWindowDays = 7
	MinLCUWindowDays     = 1
	MaxLCUWindowDays     = 60
)

// MaxLCUSerialLength bounds the serial read off the reader. Generous enough for
// the manufacturer codes in use, short enough that a pasted paragraph is caught.
const MaxLCUSerialLength = 64

// The two verdicts a single day's test can produce. The test itself is holding
// an RFID card against the reader: a green light means it read the card, red
// means it did not.
const (
	LCUResultPass = "pass"
	LCUResultFail = "fail"
)

// Unit lifecycle. A unit stays 'active' for the whole window, including after a
// day has failed: a failure is recorded and shown, but the verdict is only
// reached at the end.
const (
	LCUStatusActive = "active"
	LCUStatusPassed = "passed"
	LCUStatusFailed = "failed"
)

// LCUUnit is one card reader put on trial. Each attempt at a reader is its own
// unit, so re-testing a reader that already failed keeps its own history rather
// than overwriting the first week of results.
type LCUUnit struct {
	ID          int64      `json:"id"`
	CreatedAt   time.Time  `json:"created_at"`
	Serial      string     `json:"serial"`
	AddedBy     *int64     `json:"added_by,omitempty"`
	AddedByName string     `json:"added_by_name,omitempty"`
	StartsOn    time.Time  `json:"starts_on"`
	EndsOn      time.Time  `json:"ends_on"`
	Status      string     `json:"status"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	Note        string     `json:"note"`

	// WindowDays is how long this unit was put on trial for. It is stored on the
	// unit rather than read from the current setting, because the log below is
	// part of the unit's record: a reader trialled for a week stays a week long
	// even after a manager changes the default.
	WindowDays int `json:"window_days"`

	// Days is the log, one entry per calendar day of the window. A day with no
	// result is carried with an empty Result rather than dropped, so the caller
	// can always render the whole window and see which days are outstanding.
	Days []LCUDay `json:"days"`

	// Passes and Fails count the recorded days, out of WindowDays. The remainder
	// is how many days went untested.
	Passes int `json:"passes"`
	Fails  int `json:"fails"`
}

// LCUDay is one calendar day of a unit's window.
type LCUDay struct {
	Day          time.Time `json:"day"`
	Result       string    `json:"result,omitempty"` // empty when not tested
	Note         string    `json:"note,omitempty"`
	LoggedBy     *int64    `json:"logged_by,omitempty"`
	LoggedByName string    `json:"logged_by_name,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

// LCUTest is a stored daily result. It is only ever read as part of a unit's
// Days, so LCUDay is the API shape; this is the row behind it.
type LCUTest struct {
	ID        int64     `json:"id"`
	UnitID    int64     `json:"unit_id"`
	Day       time.Time `json:"day"`
	Result    string    `json:"result"`
	Note      string    `json:"note"`
	LoggedBy  *int64    `json:"logged_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DateOnly strips the clock and normalises to UTC, so a date column scanned
// back from Postgres -- which arrives as midnight UTC -- compares equal to a
// local "today" instead of drifting by a day.
func DateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// LCUWindow returns the calendar days of a unit's trial beginning on startsOn,
// and the closing day.
//
// The window is a run of calendar days, so it is built by adding days to a date
// rather than by adding windowDays*24h to a timestamp -- otherwise a DST change
// inside the window would silently shorten or lengthen it.
//
// days is clamped into the supported range so a bad setting can never produce a
// zero-length or absurd window; a zero would divide by nothing and panic.
func LCUWindow(startsOn time.Time, windowDays int) (days []time.Time, endsOn time.Time) {
	windowDays = ClampLCUWindowDays(windowDays)
	start := DateOnly(startsOn)
	days = make([]time.Time, windowDays)
	for i := range days {
		days[i] = start.AddDate(0, 0, i)
	}
	return days, start.AddDate(0, 0, windowDays-1)
}

// ClampLCUWindowDays forces a window length into the range the feature supports.
// Used on every path that reads one, so an out-of-range value from settings or
// from an older row cannot build a window nobody can answer.
func ClampLCUWindowDays(windowDays int) int {
	if windowDays < MinLCUWindowDays {
		return MinLCUWindowDays
	}
	if windowDays > MaxLCUWindowDays {
		return MaxLCUWindowDays
	}
	return windowDays
}

// Today is the current calendar day in the server's local time. Every date the
// LCU feature compares against goes through this one function, so the gate, the
// daily prompt and the stored results can never disagree about which day it is.
//
// It is passed to SQL as a parameter rather than relying on CURRENT_DATE on
// purpose: the database session may sit in a different timezone from the
// application, and "the day the technician logged in" is a local-calendar
// question.
func Today() time.Time {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

func ValidateLCUSerial(v *validator.Validator, serial string) {
	s := strings.TrimSpace(serial)
	v.Check(s != "", "serial", "must be provided")
	v.Check(len(s) <= MaxLCUSerialLength, "serial",
		fmt.Sprintf("must not be more than %d characters", MaxLCUSerialLength))
}

// ValidateLCUResult checks a single day's verdict.
func ValidateLCUResult(v *validator.Validator, result string) {
	v.Check(result == LCUResultPass || result == LCUResultFail,
		"result", "must be either 'pass' or 'fail'")
}

type LCUModel struct {
	DB *sql.DB
}

const lcuUnitColumns = `
	u.id, u.created_at, u.serial, u.added_by, COALESCE(adder.name, ''),
	u.starts_on, u.ends_on, u.status, u.resolved_at, u.note, u.window_days`

// scanUnitRow reads one unit row, in the column order of lcuUnitColumns. The
// daily log is attached separately by attach, because the rows have to be read
// before their ids are known.
func scanUnitRow(sc interface{ Scan(...any) error }) (*LCUUnit, error) {
	var (
		u        LCUUnit
		addedBy  sql.NullInt64
		resolved sql.NullTime
	)
	if err := sc.Scan(&u.ID, &u.CreatedAt, &u.Serial, &addedBy, &u.AddedByName,
		&u.StartsOn, &u.EndsOn, &u.Status, &resolved, &u.Note, &u.WindowDays); err != nil {
		return nil, err
	}
	if addedBy.Valid {
		u.AddedBy = &addedBy.Int64
	}
	if resolved.Valid {
		t := resolved.Time
		u.ResolvedAt = &t
	}
	// A row written before the column existed, or by hand with a nonsense value,
	// still renders a usable window rather than an empty or enormous log.
	u.WindowDays = ClampLCUWindowDays(u.WindowDays)
	return &u, nil
}

// attach fills in a unit's own window from its recorded results, and counts the
// passes and failures. Untested days are left with an empty Result so the
// window always renders in full.
func (u *LCUUnit) attach(tests []*LCUTest, loggedByNames map[int64]string) {
	byDay := make(map[string]*LCUTest, len(tests))
	for _, t := range tests {
		byDay[DateOnly(t.Day).Format("2006-01-02")] = t
	}
	window, _ := LCUWindow(u.StartsOn, u.WindowDays)
	u.Days = make([]LCUDay, 0, len(window))
	u.Passes, u.Fails = 0, 0
	for _, d := range window {
		day := LCUDay{Day: d}
		if t, found := byDay[d.Format("2006-01-02")]; found {
			day.Result = t.Result
			day.Note = t.Note
			day.LoggedBy = t.LoggedBy
			day.UpdatedAt = t.UpdatedAt
			if t.LoggedBy != nil {
				day.LoggedByName = loggedByNames[*t.LoggedBy]
			}
			switch t.Result {
			case LCUResultPass:
				u.Passes++
			case LCUResultFail:
				u.Fails++
			}
		}
		u.Days = append(u.Days, day)
	}
}

// testsForUnits loads the daily log for a set of units in one round trip, keyed
// by unit id, along with the names of whoever recorded them. Loading per unit
// would be two queries per reader on the page.
func (m LCUModel) testsForUnits(ctx context.Context, ids []int64) (map[int64][]*LCUTest, map[int64]string, error) {
	loggedByNames := make(map[int64]string)
	out := make(map[int64][]*LCUTest, len(ids))
	if len(ids) == 0 {
		return out, loggedByNames, nil
	}

	query := `
		SELECT t.id, t.unit_id, t.day, t.result, t.note, t.logged_by,
		       t.created_at, t.updated_at
		FROM lcu_tests t
		WHERE t.unit_id = ANY($1)
		ORDER BY t.day ASC`

	rows, err := m.DB.QueryContext(ctx, query, pq.Array(ids))
	if err != nil {
		return nil, nil, err
	}
	var tests []*LCUTest
	for rows.Next() {
		var (
			t        LCUTest
			loggedBy sql.NullInt64
		)
		if err := rows.Scan(&t.ID, &t.UnitID, &t.Day, &t.Result, &t.Note, &loggedBy,
			&t.CreatedAt, &t.UpdatedAt); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if loggedBy.Valid {
			t.LoggedBy = &loggedBy.Int64
		}
		tests = append(tests, &t)
		out[t.UnitID] = append(out[t.UnitID], &t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	if len(tests) > 0 {
		who, err := m.userNames(ctx, tests)
		if err != nil {
			return nil, nil, err
		}
		loggedByNames = who
	}
	return out, loggedByNames, nil
}

// userNames resolves the ids in tests to display names, so a manager's answer to
// someone else's unit shows who gave it.
func (m LCUModel) userNames(ctx context.Context, tests []*LCUTest) (map[int64]string, error) {
	seen := make(map[int64]struct{}, len(tests))
	ids := make([]int64, 0, len(tests))
	for _, t := range tests {
		if t.LoggedBy == nil {
			continue
		}
		if _, dup := seen[*t.LoggedBy]; dup {
			continue
		}
		seen[*t.LoggedBy] = struct{}{}
		ids = append(ids, *t.LoggedBy)
	}
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}

	query := `SELECT id, name FROM users WHERE id = ANY($1)`
	rows, err := m.DB.QueryContext(ctx, query, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	names := make(map[int64]string, len(ids))
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}

// Insert starts a new unit. The window opens today and runs for windowDays
// calendar days, so a reader added on the 30th with a seven-day window is judged
// on the 6th.
//
// windowDays comes from the application settings and is stored on the row, so the
// length this unit was trialled for is fixed from the moment it exists and
// survives the setting being changed later.
func (m LCUModel) Insert(u *LCUUnit, windowDays int) error {
	u.Serial = strings.TrimSpace(u.Serial)
	u.Note = strings.TrimSpace(u.Note)
	u.Status = LCUStatusActive
	u.WindowDays = ClampLCUWindowDays(windowDays)

	_, u.EndsOn = LCUWindow(u.StartsOn, u.WindowDays)
	u.StartsOn = DateOnly(u.StartsOn)

	query := `
		INSERT INTO lcu_units (serial, added_by, starts_on, ends_on, note, window_days)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return m.DB.QueryRowContext(ctx, query, u.Serial, u.AddedBy,
		u.StartsOn, u.EndsOn, u.Note, u.WindowDays).Scan(&u.ID, &u.CreatedAt)
}

// Get returns one unit with its full log, over the window it was given.
func (m LCUModel) Get(id int64) (*LCUUnit, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	u, err := m.get(ctx, id)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (m LCUModel) get(ctx context.Context, id int64) (*LCUUnit, error) {
	query := `SELECT` + lcuUnitColumns + `
		FROM lcu_units u
		LEFT JOIN users adder ON adder.id = u.added_by
		WHERE u.id = $1`

	u, err := scanUnitRow(m.DB.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	tests, names, err := m.testsForUnits(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	u.attach(tests[id], names)
	return u, nil
}

// ListAll returns every unit, newest window first. This is the manager's view.
func (m LCUModel) ListAll() ([]*LCUUnit, error) {
	return m.list(`
		FROM lcu_units u
		LEFT JOIN users adder ON adder.id = u.added_by
		ORDER BY u.starts_on DESC, u.id DESC`)
}

// ListForUser returns only the units this user added. Technicians see their own
// readers; the team-wide list is a manager's to ask for.
func (m LCUModel) ListForUser(userID int64) ([]*LCUUnit, error) {
	return m.list(`
		FROM lcu_units u
		LEFT JOIN users adder ON adder.id = u.added_by
		WHERE u.added_by = $1
		ORDER BY u.starts_on DESC, u.id DESC`, userID)
}

func (m LCUModel) list(from string, args ...any) ([]*LCUUnit, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Rows are read and closed first so their ids are all known before the daily
	// logs are fetched in a single follow-up query.
	query := `SELECT` + lcuUnitColumns + from
	rows, err := m.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var units []*LCUUnit
	for rows.Next() {
		u, err := scanUnitRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		units = append(units, u)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	ids := make([]int64, len(units))
	for i, u := range units {
		ids[i] = u.ID
	}
	tests, names, err := m.testsForUnits(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, u := range units {
		u.attach(tests[u.ID], names)
	}
	return units, nil
}

// PendingToday returns the units this user added whose window covers today and
// that have no result recorded for today. This is the list the daily prompt is
// built from, and the list the server-side gate refuses work over.
func (m LCUModel) PendingToday(userID int64, today time.Time) ([]*LCUUnit, error) {
	query := `
		SELECT u.id
		FROM lcu_units u
		WHERE u.added_by = $1
		  AND u.status = $2
		  AND $3::date BETWEEN u.starts_on AND u.ends_on
		  AND NOT EXISTS (
		      SELECT 1 FROM lcu_tests t
		      WHERE t.unit_id = u.id AND t.day = $3::date
		  )
		ORDER BY u.serial ASC`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, userID, LCUStatusActive, DateOnly(today))
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// The ids are read one at a time through get, so the prompt carries the same
	// serial and window the LCU page would show. There are only ever a handful.
	units := make([]*LCUUnit, 0, len(ids))
	for _, id := range ids {
		u, err := m.get(ctx, id)
		if err != nil {
			if errors.Is(err, ErrRecordNotFound) {
				continue
			}
			return nil, err
		}
		units = append(units, u)
	}
	return units, nil
}

// RecordTest stores a day's verdict, overwriting that day if it was already
// answered. Overwriting rather than appending is what keeps "did it pass on the
// 4th" a single unambiguous answer; created_at still shows when it was first
// given and updated_at when it was last changed.
func (m LCUModel) RecordTest(unitID int64, day time.Time, result, note string, loggedBy int64) (*LCUTest, error) {
	query := `
		INSERT INTO lcu_tests (unit_id, day, result, note, logged_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (unit_id, day) DO UPDATE
		SET result     = EXCLUDED.result,
		    note       = EXCLUDED.note,
		    logged_by  = EXCLUDED.logged_by,
		    updated_at = NOW()
		RETURNING id, unit_id, day, result, note, logged_by, created_at, updated_at`

	var (
		t          LCUTest
		recordedBy sql.NullInt64
	)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, unitID, DateOnly(day), result, note, loggedBy).
		Scan(&t.ID, &t.UnitID, &t.Day, &t.Result, &t.Note, &recordedBy, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if recordedBy.Valid {
		t.LoggedBy = &recordedBy.Int64
	}
	return &t, nil
}

// ResolveDue closes out every unit whose window has ended and gives it a
// verdict.
//
// A unit passes only if every day of *its own* window carries a passing result.
// Both sides of that comparison are per row: the pass count is bounded to the
// unit's window, and it is compared against u.window_days rather than a
// constant, so a hotel that trialled readers for a fortnight and then shortened
// the window still resolves the old fortnight-long units correctly.
//
// A day with no result is not a pass, so a unit the technician forgot about is
// not signed off -- the safe reading, since the alternative is scrapping a reader
// that may well have been fine.
//
// This runs when someone looks at the data rather than on a timer, so there is
// no background job to keep alive. Nothing depends on it having run: the gate's
// own query only ever considers units whose window still covers today.
func (m LCUModel) ResolveDue(today time.Time) (int, error) {
	query := `
		UPDATE lcu_units u
		SET status = CASE WHEN (
		        SELECT count(*) FROM lcu_tests t
		        WHERE t.unit_id = u.id AND t.result = $2
		          AND t.day BETWEEN u.starts_on AND u.ends_on
		    ) = u.window_days
		    THEN $3 ELSE $4 END,
		    resolved_at = NOW()
		WHERE u.status = $5
		  AND u.ends_on < $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, query, DateOnly(today), LCUResultPass,
		LCUStatusPassed, LCUStatusFailed, LCUStatusActive)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}

// Delete removes a unit and, by cascade, its daily log. The owner or a manager
// may do this; it exists for a reader that turned out to be unrecoverable and
// not worth the run of daily prompts.
func (m LCUModel) Delete(id int64) error {
	query := `DELETE FROM lcu_units WHERE id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrRecordNotFound
	}
	return nil
}
