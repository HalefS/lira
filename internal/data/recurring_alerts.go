package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

type RecurringAlert struct {
	ID           int64                  `json:"id"`
	Mode         string                 `json:"mode"`
	Location     string                 `json:"location"`
	Type         string                 `json:"type"`
	Status       string                 `json:"status"`
	Solution     *string                `json:"solution"`
	SolvedBy     *int64                 `json:"solved_by,omitempty"`
	SolvedByName *string                `json:"solved_by_name,omitempty"`
	SolvedAt     *time.Time             `json:"solved_at"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Issues       []*RecurringGroupIssue `json:"issues"`
}

func ValidateRecurringAlertSolution(v *validator.Validator, solution string) {
	v.Check(solution != "", "solution", "must be provided")
	v.Check(len(solution) <= 1000, "solution", "must not be more than 1000 characters")
}

type RecurringAlertModel struct {
	DB *sql.DB
}

// Sync detects every (mode, location, type) combination with 2+ issues
// logged within windowHours of now, and makes sure each one has a
// recurring_alerts row: creating a new "pending" row if none exists yet,
// or — if a group was previously marked "solved" but a new occurrence has
// happened since — flipping it back to "pending" and clearing the old
// solution, since the problem clearly wasn't actually fixed.
func (m RecurringAlertModel) Sync(windowHours int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	groupQuery := `
		SELECT mode, MAX(location) AS location, type, MAX(created_at) AS latest
		FROM issues
		WHERE created_at >= NOW() - ($1 * INTERVAL '1 hour')
		GROUP BY mode, LOWER(TRIM(location)), type
		HAVING COUNT(*) >= 2`

	rows, err := m.DB.QueryContext(ctx, groupQuery, windowHours)
	if err != nil {
		return err
	}
	type group struct {
		mode, location, issueType string
		latest                    time.Time
	}
	var groups []group
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.mode, &g.location, &g.issueType, &g.latest); err != nil {
			rows.Close()
			return err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, g := range groups {
		var (
			id       int64
			status   string
			solvedAt sql.NullTime
		)
		err := m.DB.QueryRowContext(ctx, `
			SELECT id, status, solved_at FROM recurring_alerts
			WHERE mode = $1 AND LOWER(TRIM(location)) = LOWER(TRIM($2)) AND type = $3`,
			g.mode, g.location, g.issueType,
		).Scan(&id, &status, &solvedAt)

		switch {
		case errors.Is(err, sql.ErrNoRows):
			_, err = m.DB.ExecContext(ctx, `
				INSERT INTO recurring_alerts (mode, location, type, status)
				VALUES ($1, $2, $3, 'pending')`,
				g.mode, g.location, g.issueType)
			if err != nil {
				return err
			}
		case err != nil:
			return err
		case status == "solved" && solvedAt.Valid && g.latest.After(solvedAt.Time):
			_, err = m.DB.ExecContext(ctx, `
				UPDATE recurring_alerts
				SET status = 'pending', solution = NULL, solved_by = NULL, solved_at = NULL, updated_at = NOW()
				WHERE id = $1`, id)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// List returns tracked recurring alerts (optionally filtered by status —
// "pending", "solved", or "" for all), each with the full history of
// matching issues (not limited to the detection window, since a solved
// alert's occurrences may have aged out of it).
func (m RecurringAlertModel) List(status string) ([]*RecurringAlert, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	query := `
		SELECT ra.id, ra.mode, ra.location, ra.type, ra.status, ra.solution,
		       ra.solved_by, u.name, ra.solved_at, ra.created_at, ra.updated_at
		FROM recurring_alerts ra
		LEFT JOIN users u ON ra.solved_by = u.id
		WHERE ($1 = '' OR ra.status = $1)
		ORDER BY ra.status ASC, ra.updated_at DESC`

	rows, err := m.DB.QueryContext(ctx, query, status)
	if err != nil {
		return nil, err
	}
	var alerts []*RecurringAlert
	for rows.Next() {
		var a RecurringAlert
		var solution, solvedByName sql.NullString
		var solvedBy sql.NullInt64
		var solvedAt sql.NullTime
		if err := rows.Scan(
			&a.ID, &a.Mode, &a.Location, &a.Type, &a.Status, &solution,
			&solvedBy, &solvedByName, &solvedAt, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		if solution.Valid {
			a.Solution = &solution.String
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
		alerts = append(alerts, &a)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	issueQuery := `
		SELECT i.id, i.created_at, i.problem, i.resolution, i.status, u.name
		FROM issues i
		INNER JOIN users u ON i.logged_by = u.id
		WHERE i.mode = $1 AND LOWER(TRIM(i.location)) = LOWER(TRIM($2)) AND i.type = $3
		ORDER BY i.created_at DESC`

	for _, a := range alerts {
		irows, err := m.DB.QueryContext(ctx, issueQuery, a.Mode, a.Location, a.Type)
		if err != nil {
			return nil, err
		}
		var issues []*RecurringGroupIssue
		for irows.Next() {
			var gi RecurringGroupIssue
			if err := irows.Scan(&gi.ID, &gi.CreatedAt, &gi.Problem, &gi.Resolution, &gi.Status, &gi.LoggedByName); err != nil {
				irows.Close()
				return nil, err
			}
			issues = append(issues, &gi)
		}
		if err := irows.Err(); err != nil {
			irows.Close()
			return nil, err
		}
		irows.Close()
		a.Issues = issues
	}
	return alerts, nil
}

// CountPending is used for the sidebar's unread-style badge count.
func (m RecurringAlertModel) CountPending() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var count int
	err := m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM recurring_alerts WHERE status = 'pending'`).Scan(&count)
	return count, err
}

func (m RecurringAlertModel) MarkSolved(id int64, solution string, solvedBy int64) (*RecurringAlert, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	query := `
		UPDATE recurring_alerts
		SET status = 'solved', solution = $1, solved_by = $2, solved_at = NOW(), updated_at = NOW()
		WHERE id = $3
		RETURNING id, mode, location, type, status, solution, solved_by, solved_at, created_at, updated_at`

	var a RecurringAlert
	var sol sql.NullString
	var sb sql.NullInt64
	var sa sql.NullTime
	err := m.DB.QueryRowContext(ctx, query, solution, solvedBy, id).Scan(
		&a.ID, &a.Mode, &a.Location, &a.Type, &a.Status, &sol, &sb, &sa, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	if sol.Valid {
		a.Solution = &sol.String
	}
	if sb.Valid {
		a.SolvedBy = &sb.Int64
	}
	if sa.Valid {
		a.SolvedAt = &sa.Time
	}
	return &a, nil
}

func (m RecurringAlertModel) Delete(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, `DELETE FROM recurring_alerts WHERE id = $1`, id)
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
