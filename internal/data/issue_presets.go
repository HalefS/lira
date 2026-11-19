package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
	"github.com/lib/pq"
)

type IssuePresetConsumable struct {
	ItemID   int64  `json:"item_id"`
	Item     string `json:"item,omitempty"`
	Icon     string `json:"icon,omitempty"`
	Quantity int    `json:"quantity"`
}

type IssuePreset struct {
	ID          int64                   `json:"id"`
	IssueTypeID int64                   `json:"issue_type_id"`
	IssueType   string                  `json:"issue_type,omitempty"`
	Name        string                  `json:"name"`
	Problem     string                  `json:"problem"`
	Resolution  string                  `json:"resolution"`
	Consumables []IssuePresetConsumable `json:"consumables"`
	CreatedBy   *int64                  `json:"created_by,omitempty"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

func ValidateIssuePreset(v *validator.Validator, p *IssuePreset) {
	p.Name = strings.TrimSpace(p.Name)
	p.Problem = strings.TrimSpace(p.Problem)
	p.Resolution = strings.TrimSpace(p.Resolution)

	v.Check(p.IssueTypeID >= 1, "issue_type_id", "must be provided")
	v.Check(p.Name != "", "name", "must be provided")
	v.Check(len(p.Name) <= 100, "name", "must not be more than 100 characters")
	v.Check(p.Problem != "", "problem", "must be provided")
	v.Check(len(p.Problem) <= 500, "problem", "must not be more than 500 characters")
	v.Check(len(p.Resolution) <= 500, "resolution", "must not be more than 500 characters")

	v.Check(len(p.Consumables) <= maxConsumablesPerIssue, "consumables", "must not contain more than 50 items")
	seen := make(map[int64]bool, len(p.Consumables))
	for _, c := range p.Consumables {
		v.Check(c.ItemID >= 1, "consumables", "each item must reference a valid consumable")
		v.Check(c.Quantity >= 1 && c.Quantity <= maxConsumableQuantity, "consumables", "each quantity must be between 1 and 999")
		v.Check(!seen[c.ItemID], "consumables", "the same consumable can only be listed once")
		seen[c.ItemID] = true
	}
}

type IssuePresetModel struct {
	DB *sql.DB
}

func (m IssuePresetModel) loadConsumables(ctx context.Context, presets []*IssuePreset) error {
	if len(presets) == 0 {
		return nil
	}

	byID := make(map[int64]*IssuePreset, len(presets))
	ids := make([]int64, 0, len(presets))
	for _, p := range presets {
		p.Consumables = []IssuePresetConsumable{}
		byID[p.ID] = p
		ids = append(ids, p.ID)
	}

	rows, err := m.DB.QueryContext(ctx, `
		SELECT pc.preset_id, pc.item_id, ci.name, ci.icon, pc.quantity
		FROM issue_preset_consumables pc
		INNER JOIN consumable_items ci ON ci.id = pc.item_id
		WHERE pc.preset_id = ANY($1::bigint[])
		ORDER BY ci.name ASC, pc.item_id ASC`,
		pq.Array(ids),
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var presetID int64
		var c IssuePresetConsumable
		if err := rows.Scan(&presetID, &c.ItemID, &c.Item, &c.Icon, &c.Quantity); err != nil {
			return err
		}
		if p, ok := byID[presetID]; ok {
			p.Consumables = append(p.Consumables, c)
		}
	}
	return rows.Err()
}

const issuePresetColumns = `p.id, p.issue_type_id, it.name, p.name, p.problem, p.resolution, p.created_by, p.created_at, p.updated_at`

func scanIssuePreset(scan func(dest ...any) error) (*IssuePreset, error) {
	var p IssuePreset
	var createdBy sql.NullInt64
	if err := scan(&p.ID, &p.IssueTypeID, &p.IssueType, &p.Name, &p.Problem, &p.Resolution, &createdBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		p.CreatedBy = &createdBy.Int64
	}
	p.Consumables = []IssuePresetConsumable{}
	return &p, nil
}

func (m IssuePresetModel) GetAll() ([]*IssuePreset, error) {
	query := `
		SELECT ` + issuePresetColumns + `
		FROM issue_presets p
		INNER JOIN issue_types it ON it.id = p.issue_type_id
		ORDER BY it.name ASC, p.name ASC`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var presets []*IssuePreset
	for rows.Next() {
		p, err := scanIssuePreset(rows.Scan)
		if err != nil {
			return nil, err
		}
		presets = append(presets, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := m.loadConsumables(ctx, presets); err != nil {
		return nil, err
	}
	return presets, nil
}

func (m IssuePresetModel) Get(id int64) (*IssuePreset, error) {
	query := `
		SELECT ` + issuePresetColumns + `
		FROM issue_presets p
		INNER JOIN issue_types it ON it.id = p.issue_type_id
		WHERE p.id = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p, err := scanIssuePreset(m.DB.QueryRowContext(ctx, query, id).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	if err := m.loadConsumables(ctx, []*IssuePreset{p}); err != nil {
		return nil, err
	}
	return p, nil
}

func (m IssuePresetModel) setConsumables(ctx context.Context, tx *sql.Tx, presetID int64, consumables []IssuePresetConsumable) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM issue_preset_consumables WHERE preset_id = $1`, presetID); err != nil {
		return err
	}

	for _, c := range consumables {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO issue_preset_consumables (preset_id, item_id, quantity)
			VALUES ($1, $2, $3)`,
			presetID, c.ItemID, c.Quantity,
		)
		if err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "23503" {
				return ErrRecordNotFound
			}
			return err
		}
	}
	return nil
}

func (m IssuePresetModel) Insert(p *IssuePreset) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Problem = strings.TrimSpace(p.Problem)
	p.Resolution = strings.TrimSpace(p.Resolution)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx, `
		INSERT INTO issue_presets (issue_type_id, name, problem, resolution, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`,
		p.IssueTypeID, p.Name, p.Problem, p.Resolution, p.CreatedBy,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			switch pqErr.Code {
			case "23505":
				return ErrDuplicateIssuePreset
			case "23503":
				return ErrRecordNotFound
			}
		}
		return err
	}

	if err := m.setConsumables(ctx, tx, p.ID, p.Consumables); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (m IssuePresetModel) Update(p *IssuePreset) error {
	p.Name = strings.TrimSpace(p.Name)
	p.Problem = strings.TrimSpace(p.Problem)
	p.Resolution = strings.TrimSpace(p.Resolution)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx, `
		UPDATE issue_presets
		SET issue_type_id = $1, name = $2, problem = $3, resolution = $4, updated_at = NOW()
		WHERE id = $5
		RETURNING created_at, updated_at`,
		p.IssueTypeID, p.Name, p.Problem, p.Resolution, p.ID,
	).Scan(&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrRecordNotFound
		}
		var pqErr *pq.Error
		if errors.As(err, &pqErr) {
			switch pqErr.Code {
			case "23505":
				return ErrDuplicateIssuePreset
			case "23503":
				return ErrRecordNotFound
			}
		}
		return err
	}

	if err := m.setConsumables(ctx, tx, p.ID, p.Consumables); err != nil {
		return err
	}

	return tx.Commit()
}

func (m IssuePresetModel) Delete(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, `DELETE FROM issue_presets WHERE id = $1`, id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrRecordNotFound
	}
	return nil
}
