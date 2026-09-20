package data

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Consumable struct {
	ID           int64     `json:"id"`
	IssueID      int64     `json:"issue_id"`
	Item         string    `json:"item"`
	Mode         string    `json:"mode"`
	Location     string    `json:"location"`
	LoggedBy     int64     `json:"logged_by"`
	LoggedByName string    `json:"logged_by_name"`
	CreatedAt    time.Time `json:"created_at"`
}

type ConsumableModel struct {
	DB *sql.DB
}

// UpsertForIssue records that a consumable was used for the given issue —
// inserting a new record, or updating the existing one if the issue's
// details (location/mode) have since changed via an edit.
func (m ConsumableModel) UpsertForIssue(issueID int64, item, mode, location string, loggedBy int64) error {
	query := `
		INSERT INTO consumables (issue_id, item, mode, location, logged_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (issue_id) DO UPDATE
		SET item = EXCLUDED.item, mode = EXCLUDED.mode, location = EXCLUDED.location, logged_by = EXCLUDED.logged_by`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := m.DB.ExecContext(ctx, query, issueID, item, mode, location, loggedBy)
	return err
}

// DeleteForIssue removes the consumable record for an issue, if any — used
// when the toggle is switched back off.
func (m ConsumableModel) DeleteForIssue(issueID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := m.DB.ExecContext(ctx, `DELETE FROM consumables WHERE issue_id = $1`, issueID)
	return err
}

// GetAll returns consumable records, optionally filtered to a single
// calendar date ("YYYY-MM-DD"), newest first.
func (m ConsumableModel) GetAll(date string) ([]*Consumable, error) {
	query := `
		SELECT c.id, c.issue_id, c.item, c.mode, c.location, c.logged_by, u.name, c.created_at
		FROM consumables c
		INNER JOIN users u ON c.logged_by = u.id
		WHERE ($1 = '' OR c.created_at::date = $1::date)
		ORDER BY c.created_at DESC`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Consumable
	for rows.Next() {
		var c Consumable
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Item, &c.Mode, &c.Location, &c.LoggedBy, &c.LoggedByName, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// SummaryByItem returns a total count per item — e.g. how many AA
// Batteries, Remote Controls, and Phones have been used in total.
func (m ConsumableModel) SummaryByItem() (map[string]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, `SELECT item, COUNT(*) FROM consumables GROUP BY item`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var item string
		var count int
		if err := rows.Scan(&item, &count); err != nil {
			return nil, err
		}
		out[item] = count
	}
	return out, rows.Err()
}

// GetItemForIssue returns which item (if any) is recorded for an issue.
func (m ConsumableModel) GetItemForIssue(issueID int64) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var item string
	err := m.DB.QueryRowContext(ctx, `SELECT item FROM consumables WHERE issue_id = $1`, issueID).Scan(&item)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return item, nil
}
