package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrRoomOverlap is returned when a new run would cover a room an existing run
// already covers. It is distinct from a bad request because the caller can say
// something useful about it: which run is in the way, and what it covers.
var ErrRoomOverlap = errors.New("room range overlaps an existing one")

// ErrRoomRangeTooLarge is returned for a run wider than MaxRoomRun. A fat-fingered
// "1-99999" would otherwise put a hundred thousand nonexistent rooms into the
// dropdown, and there is no way to tell them apart from real ones afterwards.
var ErrRoomRangeTooLarge = errors.New("room range covers too many rooms")

const (
	// MaxRoomRun is the widest single entry a manager may add. A floor of a
	// large hotel is a few dozen rooms, so this is generous by an order of
	// magnitude while still refusing a typo that would be unrecoverable.
	MaxRoomRun = 500

	// RoomSearchLimit caps a typeahead. The dropdown shows the first handful and
	// the field keeps filtering as more is typed, so a larger page would only
	// cost time.
	RoomSearchLimit = 12

	maxRoomDigits = 6
)

// digitsOnly matches a room number as typed in the Settings field. Anything with
// a letter, a sign, a decimal point or internal space is not a room number, and
// saying so plainly is better than a strconv error.
var digitsOnly = regexp.MustCompile(`^[0-9]{1,` + strconv.Itoa(maxRoomDigits) + `}$`)

// Room is one run of consecutive rooms. FromRoom == ToRoom is a single room.
type Room struct {
	ID        int64     `json:"id"`
	FromRoom  int       `json:"from_room"`
	ToRoom    int       `json:"to_room"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy *int64    `json:"created_by"`
	Version   int       `json:"version"`
}

// Count is how many rooms this run stands for, which is the number a manager
// actually wants to see: "1201-1212 (12 rooms)" rather than the two bounds.
func (r *Room) Count() int { return r.ToRoom - r.FromRoom + 1 }

// Label is how the run is written in the list: the bare number for one room, and
// an en-dash-free hyphen range otherwise, matching what was typed in Settings.
func (r *Room) Label() string {
	if r.FromRoom == r.ToRoom {
		return strconv.Itoa(r.FromRoom)
	}
	return strconv.Itoa(r.FromRoom) + "-" + strconv.Itoa(r.ToRoom)
}

// ParseRoomRun reads what a manager typed into the Settings field and turns it
// into a run.
//
// Two forms, and only two: a single number ("1214") or two numbers with a hyphen
// between them ("1201-1212"). Every whole number in between is part of the run,
// which is the plain reading and the one that can be checked -- a range typed
// carelessly across a floor boundary expands to every number in between and the
// caller is shown exactly how many rooms that is before anything is saved.
//
// The bound and the ordering are refused here rather than left to the database,
// because "that range is 99999 rooms" is a message about what was typed and the
// database can only say that a constraint was violated.
func ParseRoomRun(s string) (from, to int, err error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, 0, errors.New("a room number or range is required")
	}

	// Tolerate the shapes people actually type: a hyphen, an en dash, or "..".
	// Splitting on all three at once means "1201 - 1212", "1201–1212" and
	// "1201..1212" all mean the same run.
	normalised := strings.NewReplacer("–", "-", "—", "-", "..", "-", " to ", "-", "TO", "-").Replace(trimmed)
	parts := strings.Split(normalised, "-")

	clean := func(part string) (int, error) {
		part = strings.TrimSpace(part)
		if !digitsOnly.MatchString(part) {
			return 0, fmt.Errorf("%q is not a room number", part)
		}
		n, convErr := strconv.Atoi(part)
		if convErr != nil {
			return 0, fmt.Errorf("%q is not a room number", part)
		}
		return n, nil
	}

	switch len(parts) {
	case 1:
		from, err = clean(parts[0])
		if err != nil {
			return 0, 0, err
		}
		return from, from, nil
	case 2:
		from, err = clean(parts[0])
		if err != nil {
			return 0, 0, err
		}
		to, err = clean(parts[1])
		if err != nil {
			return 0, 0, err
		}
	default:
		return 0, 0, errors.New("write a room number, or a range as 1201-1212")
	}

	if from > to {
		from, to = to, from
	}
	if to-from+1 > MaxRoomRun {
		return 0, 0, fmt.Errorf("%w: %d-%d is %d rooms, and one entry may cover at most %d",
			ErrRoomRangeTooLarge, from, to, to-from+1, MaxRoomRun)
	}
	return from, to, nil
}

// Expand is the rooms a run stands for, in order. Only ever called on a run that
// ParseRoomRun has already bounded, so the length is known to be small.
func (r *Room) Expand() []int {
	out := make([]int, 0, r.Count())
	for n := r.FromRoom; n <= r.ToRoom; n++ {
		out = append(out, n)
	}
	return out
}

// RoomOverlap describes the existing run that blocked a new one, so the error
// can name it rather than just refusing.
type RoomOverlap struct {
	Existing *Room
}

func (o *RoomOverlap) Error() string {
	if o == nil || o.Existing == nil {
		return ErrRoomOverlap.Error()
	}
	return fmt.Sprintf("%s: rooms %s are already in the inventory", ErrRoomOverlap.Error(), o.Existing.Label())
}

func (o *RoomOverlap) Unwrap() error { return ErrRoomOverlap }

type RoomModel struct {
	DB *sql.DB
}

// List returns every run, narrowest first within each floor, which is the order a
// manager reads a room list in.
func (m RoomModel) List() ([]*Room, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, `
		SELECT id, from_room, to_room, created_at, created_by, version
		FROM rooms
		ORDER BY from_room, to_room`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rooms := []*Room{}
	for rows.Next() {
		r, err := scanRoom(rows)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, r)
	}
	return rooms, rows.Err()
}

func scanRoom(row interface{ Scan(...any) error }) (*Room, error) {
	var r Room
	err := row.Scan(&r.ID, &r.FromRoom, &r.ToRoom, &r.CreatedAt, &r.CreatedBy, &r.Version)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Search finds individual rooms whose number begins with q, which is what a
// typeahead wants: not runs, but the room numbers themselves.
//
// Prefix, not substring, and deliberately so. A room number carries its floor in
// the leading digits, so someone looking for 1426 types "14" -- and a substring
// search answers that with 4114 and 2414 and every other room which merely
// contains those two digits further along. Those are not candidates for the room
// being looked for; they are the rooms that made the list hard to read, and on a
// large floor they push the room actually wanted off the end of it. Matching from
// the left is also what makes the offered list predictable: what comes back is the
// floor the prefix names, in numeric order, and one more digit narrows within it.
//
// The expansion happens in the database with generate_series rather than in Go,
// so a query cannot walk every room in the inventory to filter it.
//
// more reports that the limit cut the list short. Without it the caller would
// announce "12 rooms match" when forty do, and a manager would conclude a floor is
// smaller than it is -- so one extra row is read purely to tell "that was all of
// them" apart from "that was all you are being shown".
func (m RoomModel) Search(q string, limit int) (found []int, more bool, err error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []int{}, false, nil
	}
	// A non-numeric query can still match nothing useful, but the cast below
	// would fail on it, so it is turned into a pattern that matches no number.
	pattern := roomSearchPattern(q)

	if limit <= 0 || limit > RoomSearchLimit*4 {
		limit = RoomSearchLimit
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, `
		SELECT DISTINCT n
		FROM rooms, generate_series(from_room, to_room) AS n
		WHERE n::text ILIKE $1 ESCAPE '\'
		ORDER BY n
		LIMIT $2`, pattern, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	found = []int{}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, false, err
		}
		found = append(found, n)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	if len(found) > limit {
		return found[:limit], true, nil
	}
	return found, false, nil
}

// roomSearchPattern turns a typed query into the ILIKE pattern room numbers are
// matched against.
//
// The single trailing wildcard is the whole rule: it anchors the match at the
// start of the number and nowhere else. Everything typed by the user is escaped,
// which matters more now than it did when the pattern was wrapped in wildcards on
// both sides -- an unescaped _ would otherwise match any single character and turn
// a prefix search back into the substring search it was changed to avoid.
//
// An empty query yields an empty pattern, which matches no room number. Returning
// "%" would match every room in the inventory, and Search does refuse an empty
// query before getting here -- but a pattern builder whose safety depends on its
// caller remembering that is one refactor away from offering the whole hotel to
// someone who has not decided on a room yet.
func roomSearchPattern(q string) string {
	if q == "" {
		return ""
	}
	return escapeLike(q) + "%"
}

// escapeLike neutralises the LIKE wildcards in whatever the user typed, so a
// query of "%" searches for a percent sign rather than matching everything.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// Contains reports whether a room number is in the inventory.
func (m RoomModel) Contains(room int) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var id int64
	err := m.DB.QueryRowContext(ctx,
		`SELECT id FROM rooms WHERE $1 BETWEEN from_room AND to_room LIMIT 1`, room).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CountRooms is how many rooms the inventory covers in total, for the Settings
// summary. Not the number of rows: "7 rooms" is useful and "7 entries" is not
// once ranges exist.
func (m RoomModel) CountRooms() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var total sql.NullInt64
	err := m.DB.QueryRowContext(ctx,
		`SELECT SUM(to_room - from_room + 1) FROM rooms`).Scan(&total)
	if err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return int(total.Int64), nil
}

// Get returns one run by id.
func (m RoomModel) Get(id int64) (*Room, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	row := m.DB.QueryRowContext(ctx, `
		SELECT id, from_room, to_room, created_at, created_by, version
		FROM rooms WHERE id = $1`, id)
	r, err := scanRoom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	return r, err
}

// GetByRoom returns the run that covers a room number, for the message that names
// the run in the way when an addition overlaps.
func (m RoomModel) GetByRoom(room int) (*Room, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	row := m.DB.QueryRowContext(ctx, `
		SELECT id, from_room, to_room, created_at, created_by, version
		FROM rooms WHERE $1 BETWEEN from_room AND to_room
		ORDER BY (to_room - from_room) ASC LIMIT 1`, room)
	r, err := scanRoom(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrRecordNotFound
	}
	return r, err
}

// Add inserts a run, refusing one that overlaps an existing run.
//
// The overlap read and the insert are serialised against each other by a table
// lock, because "is 1207 a room?" having two answers is the one thing this table
// must never do, and two managers saving overlapping ranges in the same second is
// exactly how that happens. A row lock would not do it: FOR UPDATE on a query that
// matched nothing locks nothing, so the second insert would sail past the check.
//
// SHARE ROW EXCLUSIVE conflicts with itself and with the ROW EXCLUSIVE a plain
// insert takes, so two Adds cannot interleave. It is held for the length of one
// save against a table of a few dozen rows, which is the right trade for being
// able to say the inventory has no duplicates in it.
func (m RoomModel) Add(from, to int, createdBy *int64) (*Room, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `LOCK TABLE rooms IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return nil, err
	}

	var clashFrom, clashTo int
	clashErr := tx.QueryRowContext(ctx, `
		SELECT from_room, to_room FROM rooms
		WHERE $1 <= to_room AND from_room <= $2
		ORDER BY from_room LIMIT 1`, from, to).Scan(&clashFrom, &clashTo)
	if clashErr == nil {
		return nil, &RoomOverlap{Existing: &Room{FromRoom: clashFrom, ToRoom: clashTo}}
	}
	if !errors.Is(clashErr, sql.ErrNoRows) {
		return nil, clashErr
	}

	var created Room
	err = tx.QueryRowContext(ctx, `
		INSERT INTO rooms (from_room, to_room, created_by)
		VALUES ($1, $2, $3)
		RETURNING id, from_room, to_room, created_at, created_by, version`,
		from, to, createdBy).Scan(
		&created.ID, &created.FromRoom, &created.ToRoom,
		&created.CreatedAt, &created.CreatedBy, &created.Version)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &created, nil
}

// Delete removes one run.
//
// Apartment issues that name a room inside it are left alone and keep their stored
// location, exactly as an issue whose department has been removed from the
// catalog keeps its department name. Refusing the delete instead would mean a
// manager could not correct a range they had added by mistake until every issue
// in those rooms had been re-filed.
func (m RoomModel) Delete(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, `DELETE FROM rooms WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		return ErrRecordNotFound
	}
	return nil
}
