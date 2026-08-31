package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

type IssueType struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Name      string    `json:"name"`
	CreatedBy *int64    `json:"created_by,omitempty"`
}

func ValidateIssueType(v *validator.Validator, it *IssueType) {
	name := strings.TrimSpace(it.Name)
	v.Check(name != "", "name", "must be provided")
	v.Check(len(name) <= 50, "name", "must not be more than 50 characters")
}

type IssueTypeModel struct {
	DB *sql.DB
}

func (m IssueTypeModel) Insert(it *IssueType) error {
	it.Name = strings.TrimSpace(it.Name)
	query := `
		INSERT INTO issue_types (name, created_by)
		VALUES ($1, $2)
		RETURNING id, created_at`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, it.Name, it.CreatedBy).Scan(&it.ID, &it.CreatedAt)
	if err != nil {
		if err.Error() == `pq: duplicate key value violates unique constraint "issue_types_name_key"` {
			return ErrDuplicateIssueType
		}
		return err
	}
	return nil
}

func (m IssueTypeModel) GetAll() ([]*IssueType, error) {
	query := `
		SELECT id, created_at, name, created_by
		FROM issue_types
		ORDER BY name ASC`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var types []*IssueType
	for rows.Next() {
		var it IssueType
		var createdBy sql.NullInt64
		if err := rows.Scan(&it.ID, &it.CreatedAt, &it.Name, &createdBy); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			it.CreatedBy = &createdBy.Int64
		}
		types = append(types, &it)
	}
	return types, rows.Err()
}

func (m IssueTypeModel) GetByName(name string) (*IssueType, error) {
	query := `
		SELECT id, created_at, name, created_by
		FROM issue_types
		WHERE name = $1`

	var it IssueType
	var createdBy sql.NullInt64
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, strings.TrimSpace(name)).Scan(
		&it.ID, &it.CreatedAt, &it.Name, &createdBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	if createdBy.Valid {
		it.CreatedBy = &createdBy.Int64
	}
	return &it, nil
}

func (m IssueTypeModel) Delete(id int64) error {
	query := `DELETE FROM issue_types WHERE id = $1`

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
