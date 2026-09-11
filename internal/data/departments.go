package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

type Department struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Name      string    `json:"name"`
	CreatedBy *int64    `json:"created_by,omitempty"`
}

func ValidateDepartment(v *validator.Validator, d *Department) {
	name := strings.TrimSpace(d.Name)
	v.Check(name != "", "name", "must be provided")
	v.Check(len(name) <= 50, "name", "must not be more than 50 characters")
}

type DepartmentModel struct {
	DB *sql.DB
}

func (m DepartmentModel) Insert(d *Department) error {
	d.Name = strings.TrimSpace(d.Name)
	query := `
		INSERT INTO departments (name, created_by)
		VALUES ($1, $2)
		RETURNING id, created_at`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, d.Name, d.CreatedBy).Scan(&d.ID, &d.CreatedAt)
	if err != nil {
		if err.Error() == `pq: duplicate key value violates unique constraint "departments_name_key"` {
			return ErrDuplicateDepartment
		}
		return err
	}
	return nil
}

func (m DepartmentModel) GetAll() ([]*Department, error) {
	query := `
		SELECT id, created_at, name, created_by
		FROM departments
		ORDER BY name ASC`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var depts []*Department
	for rows.Next() {
		var d Department
		var createdBy sql.NullInt64
		if err := rows.Scan(&d.ID, &d.CreatedAt, &d.Name, &createdBy); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			d.CreatedBy = &createdBy.Int64
		}
		depts = append(depts, &d)
	}
	return depts, rows.Err()
}

func (m DepartmentModel) GetByName(name string) (*Department, error) {
	query := `
		SELECT id, created_at, name, created_by
		FROM departments
		WHERE name = $1`

	var d Department
	var createdBy sql.NullInt64
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, strings.TrimSpace(name)).Scan(
		&d.ID, &d.CreatedAt, &d.Name, &createdBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	if createdBy.Valid {
		d.CreatedBy = &createdBy.Int64
	}
	return &d, nil
}

func (m DepartmentModel) Delete(id int64) error {
	query := `DELETE FROM departments WHERE id = $1`

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
