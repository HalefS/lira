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
		if isDuplicateDepartment(err) {
			return ErrDuplicateDepartment
		}
		return err
	}
	return nil
}

// Rename changes a department's name and carries that name to everything that
// kept a copy of it.
//
// The copies are the whole difficulty. Issues and recurring alerts store a
// department as text rather than as the department's id, and both are matched on
// that text: recurring-fault detection looks for the same (mode, location, type)
// triple. Renaming the department and leaving those behind would not merely be
// untidy -- it would split one physical location into two, quietly reset the
// recurring history for it, and stop an open recurring alert from matching any
// issue logged afterwards. So the name goes with it.
//
// maintenance_schedules holds a real foreign key and needs no help; it follows
// the rename on its own.
//
// One transaction for all three. A rename that succeeded on departments and
// failed on issues would leave the data in exactly the split state this exists to
// prevent, and there is no undo for a half-applied rename.
//
// Returns how many issues were rewritten, so the caller can tell the reader
// whether anything moved rather than silently changing history.
func (m DepartmentModel) Rename(d *Department) (int, error) {
	d.Name = strings.TrimSpace(d.Name)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var previous string
	var createdAt time.Time
	var createdBy sql.NullInt64
	err = tx.QueryRowContext(ctx,
		`SELECT name, created_at, created_by FROM departments WHERE id = $1`, d.ID).
		Scan(&previous, &createdAt, &createdBy)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrRecordNotFound
	}
	if err != nil {
		return 0, err
	}
	// Filled from the row just read, so the answer carries the department's real
	// details rather than a zero timestamp for the ones this call did not touch.
	d.CreatedAt = createdAt
	if createdBy.Valid {
		id := createdBy.Int64
		d.CreatedBy = &id
	}

	if previous != d.Name {
		// The department row first: it is the one with the unique constraint, so
		// colliding with an existing name fails here, before anything has been
		// rewritten.
		res, err := tx.ExecContext(ctx, `UPDATE departments SET name = $1 WHERE id = $2`, d.Name, d.ID)
		if err != nil {
			if isDuplicateDepartment(err) {
				return 0, ErrDuplicateDepartment
			}
			return 0, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return 0, ErrRecordNotFound
		}

		// Only department-mode rows. An apartment location that happens to read
		// like a department name is a different place entirely and must not be
		// dragged along with the rename.
		issues, err := tx.ExecContext(ctx,
			`UPDATE issues SET location = $1 WHERE mode = 'dept' AND location = $2`, d.Name, previous)
		if err != nil {
			return 0, err
		}
		moved, _ := issues.RowsAffected()

		// A recurring alert is derived from the location it was raised for. Left
		// behind it would keep watching a name nothing logs against any more.
		//
		// That move can collide. The alert index is unique on
		// (mode, lower(location), type), so a rename onto a name that already
		// has an alert of the same type cannot be applied. The transaction rolls
		// back, which is right -- but the manager needs to be told what collided
		// rather than shown a 500.
		if _, err := tx.ExecContext(ctx,
			`UPDATE recurring_alerts SET location = $1 WHERE mode = 'dept' AND location = $2`, d.Name, previous); err != nil {
			if isRecurringAlertDuplicate(err) {
				return 0, ErrDepartmentRenameClash
			}
			return 0, err
		}

		if err := tx.Commit(); err != nil {
			return 0, err
		}
		return int(moved), nil
	}

	// Renamed to the name it already had: nothing to carry, and no write.
	return 0, tx.Commit()
}

// isDuplicateDepartment recognises the unique-constraint violation on
// departments.name.
//
// Matched on the error text rather than on the SQLSTATE because the shape of the
// message is the pq driver detail this repository already depends on elsewhere;
// the same string is checked in Insert for the same reason.
func isDuplicateDepartment(err error) bool {
	return err.Error() == `pq: duplicate key value violates unique constraint "departments_name_key"`
}

func isRecurringAlertDuplicate(err error) bool {
	return err.Error() == `pq: duplicate key value violates unique constraint "recurring_alerts_key_idx"`
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

// Get returns one department by id, or ErrRecordNotFound.
//
// Exists alongside GetByName because rows that reference a department hold its
// id, not its name -- so validating such a reference needs to fetch by id, and
// finding that id among everything would be both slower and a worse error.
func (m DepartmentModel) Get(id int64) (*Department, error) {
	query := `
		SELECT id, created_at, name, created_by
		FROM departments
		WHERE id = $1`

	var d Department
	var createdBy sql.NullInt64
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, id).Scan(
		&d.ID, &d.CreatedAt, &d.Name, &createdBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	if createdBy.Valid {
		v := createdBy.Int64
		d.CreatedBy = &v
	}
	return &d, nil
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
