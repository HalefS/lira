package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
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

// ErrUnitInReconditionedPool is returned when a reader that is in the pool is
// deleted without the caller saying the reader has physically left.
//
// The pool is a count of readers in a cupboard. Deleting a trial row is tidying
// paperwork -- it does not scrap a reader and it does not take one off a shelf --
// so allowing it silently would leave the number disagreeing with the stock, which
// is the one thing a stock number must never do. The caller has to mean it.
var ErrUnitInReconditionedPool = errors.New("lcu: reader is in the reconditioned pool")

// ReconditionedPool is the size of the pool, split by who put the reader there.
//
// Pool is TEAM-WIDE for every caller, and Yours is scoped. They answer different
// questions and they are deliberately not the same number:
//
//	Pool  "how many known-good readers are in the cupboard" is a fact about the
//	      HOTEL. A technician deciding whether to swap a reader needs forty, not the
//	      three they personally trialled.
//	Yours "how many did I add" is about the caller, and is what makes a credit
//	      attributable to a person rather than to a sweep that ran on someone's page
//	      load.
//
// Keeping them apart is also the only way "you added 3" cannot be read as "the pool
// has 3". Two numbers on one surface invite exactly that confusion and no wording
// fixes it; the layout has to do it, which is why they are two stat cards.
type ReconditionedPool struct {
	Pool  int `json:"pool"`
	Yours int `json:"yours"`
}

// LCUCredit is one reader newly added to the reconditioned pool by a sweep.
//
// This exists so the UI can say "+1" at the moment it happened rather than the
// client diffing two lists it happened to have held. It CANNOT be derived in the
// browser: the credit is made server-side by whichever request ran ResolveDue, and
// the client was not holding the prior state. The app fires /vcu/today on every boot
// before anything else, so a sweep performed by a request the user never looks at
// must still be reportable by the next one that does -- which is why ResolveDue is
// called from the resting endpoint alone, and why that endpoint's response is the
// only place a credit can be observed.
//
// Serial is the durable identity here, not UnitID: the ledger keeps a reader whose
// trial row has since been deleted (migration 000034), and UnitID is nullable for
// exactly that reason.
//
// AddedBy is who put the READER ON TRIAL, copied from the unit. It is deliberately
// NOT who ran the sweep -- ResolveDue fires on whichever request happened to run it,
// so crediting that user would put a cupboard decision in a colleague's name.
type LCUCredit struct {
	UnitID      *int64    `json:"unit_id,omitempty"`
	Serial      string    `json:"serial"`
	CountedAt   time.Time `json:"counted_at"`
	AddedBy     *int64    `json:"added_by,omitempty"`
	AddedByName string    `json:"added_by_name,omitempty"`
}

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

	// InPool and PoolCountedAt say whether this reader is one of the ones counted
	// in the reconditioned pool, and when it joined. They are only populated by the
	// RESTING list, and are the reason the client never has to reconcile the resting
	// rows against a separate pool list: a row can render "in the pool" or "cannot be
	// deleted" without asking anything else.
	//
	// A passed unit can be InPool false, and that is not a bug: units that resolved
	// before migration 000034 exists, or whose serial was since reused by a different
	// physical reader, are not in the ledger. The UI's delete affordance keys off this
	// field precisely because "a resolved unit that is not pool-bound is still
	// deletable" is a real case, not a hypothetical.
	InPool        bool       `json:"in_pool"`
	PoolCountedAt *time.Time `json:"pool_counted_at,omitempty"`
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
//
// extra takes destinations for any columns the caller appended to the SELECT AFTER
// lcuUnitColumns. database/sql requires one destination per returned column in a
// single Scan call, so the resting list's two pool columns cannot be read by a
// second Scan on the same rows -- they have to travel with the first one. Hence a
// parameter rather than two Scan calls.
func scanUnitRow(sc interface{ Scan(...any) error }, extra ...any) (*LCUUnit, error) {
	var (
		u        LCUUnit
		addedBy  sql.NullInt64
		resolved sql.NullTime
	)
	dest := []any{&u.ID, &u.CreatedAt, &u.Serial, &addedBy, &u.AddedByName,
		&u.StartsOn, &u.EndsOn, &u.Status, &resolved, &u.Note, &u.WindowDays}
	if err := sc.Scan(append(dest, extra...)...); err != nil {
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

// ListAll returns every unit still ON TRIAL, newest window first. This is the
// manager's view.
//
// The status filter is new. It used to return resolved units too, which was harmless
// when there was nowhere else to see them and is a duplicate list now that the
// resting section exists -- the same reader appearing twice on one page, once as live
// work with a live "record today's result" button and once as a closed record.
func (m LCUModel) ListAll(limit int) ([]*LCUUnit, int, error) {
	return m.list(lcuList{
		from: `
		FROM lcu_units u
		LEFT JOIN users adder ON adder.id = u.added_by
		WHERE u.status = $1`,
		args:    []any{LCUStatusActive},
		orderBy: "u.starts_on DESC, u.id DESC",
	}, limit)
}

// ListForUser returns only the ON-TRIAL units this user added. Technicians see their
// own readers; the team-wide list is a manager's to ask for.
func (m LCUModel) ListForUser(userID int64, limit int) ([]*LCUUnit, int, error) {
	return m.list(lcuList{
		from: `
		FROM lcu_units u
		LEFT JOIN users adder ON adder.id = u.added_by
		WHERE u.added_by = $1 AND u.status = $2`,
		args:    []any{userID, LCUStatusActive},
		orderBy: "u.starts_on DESC, u.id DESC",
	}, limit)
}

// ListRestedAll is the manager's resting history: every reader whose trial has ended,
// newest verdict first.
func (m LCUModel) ListRestedAll(limit int) ([]*LCUUnit, int, error) {
	return m.list(restingList(nil), limit)
}

// ListRestedForUser is the same history scoped to one technician. A reader's week of
// results is nobody else's business, and the existing per-unit rule says so; the
// POOL COUNT stays team-wide even so, because that number is a fact about the hotel.
// See ReconditionedPool.
func (m LCUModel) ListRestedForUser(userID int64, limit int) ([]*LCUUnit, int, error) {
	return m.list(restingList([]any{userID}), limit)
}

// restingList builds the shared FROM/WHERE for the resting section, optionally
// scoped to one adder.
//
// status <> 'active' rather than resolved_at IS NOT NULL: the two are equivalent
// today, and status is the CHECK-constrained column whose only writer is
// ResolveDue. A section that appeared because some future write touched a timestamp
// for an unrelated reason is a section nobody trusts.
func restingList(scope []any) lcuList {
	where := "u.status <> '" + LCUStatusActive + "'"
	args := []any{}
	if len(scope) == 1 {
		where += " AND u.added_by = $1"
		args = scope
	}
	return lcuList{
		from: `
		FROM lcu_units u
		LEFT JOIN users adder ON adder.id = u.added_by
		LEFT JOIN lcu_reconditioned rc ON rc.unit_id = u.id
		WHERE ` + where,
		args:     args,
		orderBy:  "u.resolved_at DESC, u.id DESC",
		withPool: true,
	}
}

// lcuList parameterises list().
//
// It is a struct rather than a fifth positional argument because the two lists
// genuinely differ along three axes -- what they filter, how they order, and whether
// they carry the pool columns -- and three bare bools and strings in a row is how the
// wrong one ends up on the wrong caller. orderBy is NOT optional: the resting section
// must order by resolved_at and the active list must not, and a shared default would
// have quietly been wrong for one of them.
type lcuList struct {
	from     string
	args     []any
	orderBy  string
	withPool bool
}

// list returns up to limit units plus the total that matched. The FROM/WHERE and the
// matching arguments travel together in lcuList so the count query reuses the very
// same strings rather than having the filters written twice -- a count that could
// disagree with the rows it is counting for is worse than no count.
func (m LCUModel) list(q lcuList, limit int) ([]*LCUUnit, int, error) {
	ctx, cancel := listContext()
	defer cancel()

	total, err := CountMatching(ctx, m.DB, q.from, q.args)
	if err != nil {
		return nil, 0, err
	}

	// The pool columns are appended AFTER lcuUnitColumns rather than folded into it,
	// so the active list's scan is untouched and cannot accidentally be handed two
	// extra values it does not read.
	columns := lcuUnitColumns
	if q.withPool {
		columns += `, (rc.id IS NOT NULL) AS in_pool, rc.counted_at`
	}

	// Rows are read and closed first so their ids are all known before the daily
	// logs are fetched in a single follow-up query.
	query := `SELECT ` + columns + q.from + `
		ORDER BY ` + q.orderBy + `
		LIMIT $` + strconv.Itoa(len(q.args)+1)
	rows, err := m.DB.QueryContext(ctx, query, append(append([]any{}, q.args...), limit)...)
	if err != nil {
		return nil, 0, err
	}
	units := []*LCUUnit{}
	for rows.Next() {
		// The pool columns travel with the FIRST Scan rather than a second one --
		// database/sql wants one destination per returned column per call -- which is
		// why scanUnitRow takes them as extra arguments instead of the row being
		// scanned twice.
		var (
			inPool  bool
			counted sql.NullTime
		)
		extra := []any{}
		if q.withPool {
			extra = []any{&inPool, &counted}
		}
		u, err := scanUnitRow(rows, extra...)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		if q.withPool {
			u.InPool = inPool
			if counted.Valid {
				ct := counted.Time
				u.PoolCountedAt = &ct
			}
		}
		units = append(units, u)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()

	ids := make([]int64, len(units))
	for i, u := range units {
		ids[i] = u.ID
	}
	tests, names, err := m.testsForUnits(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	for _, u := range units {
		u.attach(tests[u.ID], names)
	}
	return units, total, nil
}

// ReconditionedPool returns the pool size, team-wide and per-caller, in ONE query.
//
// One query rather than two because the two numbers are shown side by side and a
// layout that implies a relationship between them must not be assembled from two
// reads that could disagree.
func (m LCUModel) ReconditionedPool(ctx context.Context, userID int64) (*ReconditionedPool, error) {
	var pool, yours int
	err := m.DB.QueryRowContext(ctx, `
		SELECT count(*) FILTER (WHERE added_by = $1),
		       count(*)
		FROM lcu_reconditioned`, userID).Scan(&yours, &pool)
	if err != nil {
		return nil, err
	}
	return &ReconditionedPool{Pool: pool, Yours: yours}, nil
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

// ResolveDue closes out every unit whose window has ended, gives it a verdict, and
// credits every reader that passed into the reconditioned pool.
//
// A unit passes only if every day of *its own* window carries a passing result.
// Both sides of that comparison are per row: the pass count is bounded to the
// unit's window, and it is compared against u.window_days rather than a
// constant, so a hotel that trialled readers for a fortnight and then shortened the
// window still resolves the old fortnight-long units correctly.
//
// A day with no result is not a pass, so a unit the technician forgot about is
// not signed off -- the safe reading, since the alternative is scrapping a reader
// that may well have been fine.
//
// This runs when someone looks at the data rather than on a timer, so there is no
// background job to keep alive. Nothing depends on it having run: the gate's own
// query only ever considers units whose window still covers today, by DATE RANGE
// and not by status (PendingToday), so the app is not locked either way.
//
// # WHY THE CREDIT IS ONE STATEMENT AND NOT TWO
//
// A data-modifying CTE is ONE statement, which is one implicit transaction: the
// UPDATE and the INSERT commit or abort together, so there is no instant at which a
// reader is passed but uncounted. Wrapping it in BEGIN/COMMIT in Go would buy
// nothing over that and would cost a second round trip.
//
// # WHY IT IS EXACTLY ONCE
//
// Three independent mechanisms, and the first is doing the real work:
//
//  1. The UPDATE's predicate. Under READ COMMITTED a second concurrent transaction's
//     UPDATE blocks on the row lock, then re-evaluates the predicate against the NEW
//     row version -- sees status='passed', fails status='active', and skips the row.
//     It never enters `resolved`, so it never attempts an insert.
//  2. ON CONFLICT DO NOTHING on the unique serial index. Even if (1) failed, a
//     duplicate is a no-op rather than an error or a second row.
//  3. That index is on the NORMALISED SERIAL, not on unit_id, so it also absorbs the
//     re-trial case: one reader trialled twice and passed twice is one pool entry.
//
// DO NOTHING rather than DO UPDATE is deliberate. A conflict means somebody already
// recorded this reader, and there is no later-arriving truth that should overwrite
// what is there.
//
// Returns the units resolved (passed and failed together, as before) AND the pool
// entries this sweep created. Callers that only need the old behaviour ignore the
// second value; the resting handler uses it to tell the UI what was just added.
func (m LCUModel) ResolveDue(today time.Time) (int, []*LCUCredit, error) {
	// One statement, one round trip, and both counts out of the same snapshot.
	//
	// The outer SELECT emits a row per RESOLVED unit -- so its row count is the
	// number of windows closed out, which is what this method has always returned and
	// what its callers mean by it. The credits ride along on that row, so a failed
	// unit is simply a row with was_credited false. Counting only the INSERT's
	// RETURNING would have silently changed the meaning of the first return value
	// from "units resolved" to "readers added to the pool", which is a different
	// number and a different question.
	//
	// Postgres runs every data-modifying CTE in a WITH exactly once and to completion,
	// whether or not the primary query reads its output, so `credited` is not
	// conditional on being referenced.
	query := `
		WITH resolved AS (
			UPDATE lcu_units u
			SET status = CASE WHEN (
			        SELECT count(*) FROM lcu_tests t
			        WHERE t.unit_id = u.id AND t.result = $2
			          AND t.day BETWEEN u.starts_on AND u.ends_on
			    ) = u.window_days
			    THEN $3 ELSE $4 END,
			    resolved_at = NOW()
			WHERE u.status = $5
			  AND u.ends_on < $1
			RETURNING u.id, u.serial, u.added_by, u.status
		), credited AS (
			INSERT INTO lcu_reconditioned (serial, unit_id, added_by)
			SELECT r.serial, r.id, r.added_by
			FROM resolved r
			WHERE r.status = $3
			ON CONFLICT (lower(trim(serial))) DO NOTHING
			RETURNING unit_id, counted_at
		)
		SELECT r.id, r.serial, r.added_by,
		       (c.unit_id IS NOT NULL) AS was_credited,
		       c.counted_at
		FROM resolved r
		LEFT JOIN credited c ON c.unit_id = r.id`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, DateOnly(today), LCUResultPass,
		LCUStatusPassed, LCUStatusFailed, LCUStatusActive)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	resolved := 0
	credited := []*LCUCredit{}
	for rows.Next() {
		var (
			id      int64
			serial  string
			addedBy sql.NullInt64
			wasCred bool
			counted sql.NullTime
		)
		if err := rows.Scan(&id, &serial, &addedBy, &wasCred, &counted); err != nil {
			return 0, nil, err
		}
		resolved++
		if !wasCred {
			continue
		}
		c := &LCUCredit{UnitID: &id, Serial: serial}
		if counted.Valid {
			c.CountedAt = counted.Time
		}
		if addedBy.Valid {
			aid := addedBy.Int64
			c.AddedBy = &aid
		}
		credited = append(credited, c)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	if err := m.fillCreditNames(ctx, credited); err != nil {
		return 0, nil, err
	}
	return resolved, credited, nil
}

// fillCreditNames resolves the adder's display name for the credits a sweep just
// made. It runs over a handful of rows on a code path that is at most once per
// request, so the extra round trip is cheaper than complicating the CTE with a
// fourth join whose only purpose is a name nobody reads past the toast.
func (m LCUModel) fillCreditNames(ctx context.Context, credits []*LCUCredit) error {
	if len(credits) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(credits))
	for _, c := range credits {
		if c.AddedBy != nil {
			ids = append(ids, *c.AddedBy)
		}
	}
	names, err := m.userNames(ctx, creditsAsTests(ids))
	if err != nil {
		return err
	}
	for _, c := range credits {
		if c.AddedBy != nil {
			c.AddedByName = names[*c.AddedBy]
		}
	}
	return nil
}

// creditsAsTests is a shim so the existing userNames lookup -- which takes the tests
// it collected while reading rows -- can be reused for a handful of user ids.
//
// It exists only because userNames' signature predates this caller and changing it
// would mean touching every LCU read path. Given that it allocates one slice per
// sweep of at most MaxLCUSweepCredits rows, that is the cheaper trade.
func creditsAsTests(ids []int64) []*LCUTest {
	out := make([]*LCUTest, 0, len(ids))
	for _, id := range ids {
		out = append(out, &LCUTest{LoggedBy: &id})
	}
	return out
}

// Delete removes a unit and, by cascade, its daily log. The owner or a manager
// may do this; it exists for a reader that turned out to be unrecoverable and
// not worth the run of daily prompts.
//
// A reader IN THE RECONDITIONED POOL is refused unless force is set. Deleting a
// trial row is tidying paperwork -- it does not scrap a reader, and it does not take
// one off a shelf -- so allowing it silently would leave the pool count disagreeing
// with the stock, which is the one thing a stock number must never do. The caller
// has to say the reader has physically left.
//
// force is deliberately a separate act rather than a question asked by this layer:
// "delete this row" and "this hardware is gone" are different facts about the world,
// and one dialog conflating them would let the first answer stand in for the second.
func (m LCUModel) Delete(id int64, force bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if !force {
		var inPool bool
		// Matched on the NORMALISED serial rather than unit_id, because the ledger
		// keeps its serial after a trial row is gone and a reader re-trialled under a
		// new unit_id is still the same reader in the cupboard.
		err := m.DB.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM lcu_reconditioned rc
				JOIN lcu_units u ON lower(trim(u.serial)) = lower(trim(rc.serial))
				WHERE u.id = $1)`, id).Scan(&inPool)
		if err != nil {
			return err
		}
		if inPool {
			return ErrUnitInReconditionedPool
		}
	}

	query := `DELETE FROM lcu_units WHERE id = $1`

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
