package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

// maxTVSwapAlertSolutionLength bounds the note left when a faulty set is signed
// off, matching the bound on a recurring alert's solution.
const maxTVSwapAlertSolutionLength = 1000

// TVSwapAlert is one outstanding "this room's equipment is still broken" warning,
// raised by one TV swap.
//
// Only the verdict (status, solution, who and when) is stored. Everything else
// is read from the swap and its issue at the moment the page asks for it, so
// editing the issue or the swap changes the alert rather than leaving behind a
// copy of what it used to say. That is the whole reason this is not a snapshot.
type TVSwapAlert struct {
	ID      int64  `json:"id"`
	SwapID  int64  `json:"swap_id"`
	IssueID int64  `json:"issue_id"`
	Status  string `json:"status"`

	// The fault's own details, joined in from the swap and its issue.
	Mode     string `json:"mode"`
	Location string `json:"location"`
	Type     string `json:"type"`
	Problem  string `json:"problem"`

	// Which set was carried where. ToRoom is usually Location and FromRoom is a
	// healthy room, but not always -- a set can be moved to a room whose own set
	// was fine.
	FromRoom       string    `json:"from_room"`
	ToRoom         string    `json:"to_room"`
	Notes          string    `json:"notes"`
	SwapCreatedAt  time.Time `json:"swap_created_at"`
	IssueCreatedAt time.Time `json:"issue_created_at"`
	LoggedByName   string    `json:"logged_by_name"`
	SwapVersion    int       `json:"swap_version"`

	// The verdict. Solution is empty rather than null while pending.
	Solution     string     `json:"solution"`
	SolvedBy     *int64     `json:"solved_by,omitempty"`
	SolvedByName *string    `json:"solved_by_name,omitempty"`
	SolvedAt     *time.Time `json:"solved_at"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ValidateTVSwapAlertSolution checks the note left when a faulty set is signed
// off. A note that is only whitespace is refused rather than stored: it reads as
// a solution on the card while recording nothing, and the page would then show a
// solved alert whose only explanation is blank.
//
// This is checked against the trimmed value, so callers should store what they
// validated (see the handler, which trims before calling).
func ValidateTVSwapAlertSolution(v *validator.Validator, solution string) {
	trimmed := strings.TrimSpace(solution)
	v.Check(trimmed != "", "solution", "must be provided")
	v.Check(len(trimmed) <= maxTVSwapAlertSolutionLength, "solution",
		"must not be more than 1000 characters")
}

type TVSwapAlertModel struct {
	DB *sql.DB
}

// liveSwapQuery is every swap that should be raising an alert.
//
// Deliberately NOT filtered on the issue's status. An issue with a swap is
// normally closed in the same breath it is logged -- the guest has a working set
// now, so the issue genuinely is fixed -- while the faulty set is still sitting
// in storage waiting to be repaired. That is precisely the case this alert
// exists for, so gating on Pending meant the common case raised nothing at all.
//
// The alert is therefore about the equipment, not the issue. It is cleared by
// the manager signing it off, or by the swap (or its issue) being deleted, and
// by nothing else.
const liveSwapQuery = `
	FROM issue_tv_swaps s`

// Sync reconciles the stored verdicts with the swaps that actually exist. It
// runs when the Alerts page is read rather than on a timer, like the recurring
// alerts' Sync, so there is no background job to keep alive.
//
// Three things have to happen, and doing them as separate statements keeps each
// one legible:
//
//  1. drop alerts whose swap has gone. Deleting an issue cascades to its swaps
//     and then to these rows, but the explicit delete is belt-and-braces and
//     tidies up any row a cascade somehow left behind.
//  2. create a pending alert for every swap that has none.
//  3. re-open a solved alert whose swap has been edited since the verdict was
//     recorded. Someone who corrects the rooms or the notes after signing the
//     fault off has changed what the alert was about, so the old verdict no
//     longer describes it.
func (m TVSwapAlertModel) Sync() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Alerts whose swap no longer exists.
	prune := `DELETE FROM tv_swap_alerts a
		WHERE NOT EXISTS (
			SELECT 1 FROM issue_tv_swaps s WHERE s.id = a.swap_id
		)`
	if _, err := m.DB.ExecContext(ctx, prune); err != nil {
		return err
	}

	// 2. A pending alert for every swap that lacks one. ON CONFLICT DO NOTHING
	// rather than DO UPDATE: a solved alert that is still accurate must keep its
	// verdict, and step 3 is the only thing allowed to clear one.
	create := `INSERT INTO tv_swap_alerts (swap_id)
		SELECT s.id ` + liveSwapQuery + `
		ON CONFLICT (swap_id) DO NOTHING`
	if _, err := m.DB.ExecContext(ctx, create); err != nil {
		return err
	}

	// 3. Re-open a verdict that the swap has since outgrown. The issue's own
	// edits are not a reason to re-open -- what this alert is about is the set
	// that was carried, and a corrected location or problem text does not change
	// that -- but a corrected room, note or swap does.
	reopen := `UPDATE tv_swap_alerts a
		SET status = 'pending',
		    solution = '',
		    solved_by = NULL,
		    solved_at = NULL,
		    solved_swap_version = 0,
		    updated_at = NOW()
		FROM issue_tv_swaps s
		WHERE s.id = a.swap_id
		  AND a.status = 'solved'
		  AND s.version > a.solved_swap_version`
	_, err := m.DB.ExecContext(ctx, reopen)
	return err
}

// tvSwapAlertSelect is the one shape an alert is read in. List and get share it
// so a single alert can never come back with a different set of fields from a
// list of them.
const tvSwapAlertSelect = `
	SELECT a.id, a.swap_id, s.issue_id, a.status,
	       i.mode, i.location, i.type, i.problem,
	       s.from_room, s.to_room, s.notes, s.created_at, s.version,
	       i.created_at, COALESCE(logger.name, ''),
	       a.solution, a.solved_by, COALESCE(solver.name, ''), a.solved_at,
	       a.created_at, a.updated_at
	FROM tv_swap_alerts a
	JOIN issue_tv_swaps s ON s.id = a.swap_id
	JOIN issues i ON i.id = s.issue_id
	LEFT JOIN users logger ON logger.id = i.logged_by
	LEFT JOIN users solver ON solver.id = a.solved_by`

// scanAlert reads one alert row in the column order of tvSwapAlertSelect.
func scanAlert(sc interface{ Scan(...any) error }) (*TVSwapAlert, error) {
	var a TVSwapAlert
	var solvedBy sql.NullInt64
	var solvedByName sql.NullString
	var solvedAt sql.NullTime
	if err := sc.Scan(
		&a.ID, &a.SwapID, &a.IssueID, &a.Status,
		&a.Mode, &a.Location, &a.Type, &a.Problem,
		&a.FromRoom, &a.ToRoom, &a.Notes, &a.SwapCreatedAt, &a.SwapVersion,
		&a.IssueCreatedAt, &a.LoggedByName,
		&a.Solution, &solvedBy, &solvedByName, &solvedAt,
		&a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if solvedBy.Valid {
		a.SolvedBy = &solvedBy.Int64
	}
	if solvedByName.Valid {
		a.SolvedByName = &solvedByName.String
	}
	if solvedAt.Valid {
		a.SolvedAt = &solvedAt.Time
	}
	return &a, nil
}

// Get returns one alert in the full shape, which is what MarkSolved hands back.
func (m TVSwapAlertModel) Get(id int64) (*TVSwapAlert, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	row := m.DB.QueryRowContext(ctx, tvSwapAlertSelect+` WHERE a.id = $1`, id)
	a, err := scanAlert(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return a, nil
}

// List returns the alerts, optionally filtered by status ("pending", "solved", or
// "" for all), newest activity first.
func (m TVSwapAlertModel) List(status string) ([]*TVSwapAlert, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := tvSwapAlertSelect + `
		WHERE ($1 = '' OR a.status = $1)
		ORDER BY a.status ASC, a.updated_at DESC, a.id DESC`

	rows, err := m.DB.QueryContext(ctx, query, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Initialised rather than nil so the JSON is always an array: the Alerts page
	// reads .length on this, and a null there would take the section down.
	alerts := []*TVSwapAlert{}
	for rows.Next() {
		a, err := scanAlert(rows)
		if err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

// CountPending is the number shown beside the section heading.
func (m TVSwapAlertModel) CountPending() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var count int
	err := m.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tv_swap_alerts WHERE status = 'pending'`).Scan(&count)
	return count, err
}

// MarkSolved records that the faulty equipment was dealt with, stamping the
// verdict against the swap's current version. If the swap is edited afterwards
// the version moves past it and Sync re-opens the alert.
//
// It reads the row back through the shared select and returns the whole alert
// rather than just the columns it changed. The caller merges that response over
// the alert it already holds, and a partial object would overwrite the joined
// fields (room, type, problem, the swap's own rooms) with Go's zero values --
// which is not a cosmetic difference, since those are what the card is made of.
func (m TVSwapAlertModel) MarkSolved(id int64, solution string, solvedBy int64) (*TVSwapAlert, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		UPDATE tv_swap_alerts a
		SET status = 'solved',
		    solution = $1,
		    solved_by = $2,
		    solved_at = NOW(),
		    updated_at = NOW(),
		    solved_swap_version = COALESCE(
		        (SELECT s.version FROM issue_tv_swaps s WHERE s.id = a.swap_id), 0)
		WHERE a.id = $3
		RETURNING a.id`

	var updatedID int64
	err := m.DB.QueryRowContext(ctx, query, solution, solvedBy, id).Scan(&updatedID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	// Read back through the shared shape rather than assembling a partial object.
	return m.Get(updatedID)
}

// Delete drops the alert but not the swap: the swap is the record that a set was
// moved, and that stays whatever anyone thinks of the alert. This is for an alert
// raised by mistake, not for clearing a real fault.
func (m TVSwapAlertModel) Delete(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, `DELETE FROM tv_swap_alerts WHERE id = $1`, id)
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
