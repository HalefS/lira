package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

type ConnectaAgent struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Name      string    `json:"name"`
	CreatedBy *int64    `json:"created_by,omitempty"`
}

func ValidateConnectaAgent(v *validator.Validator, a *ConnectaAgent) {
	name := strings.TrimSpace(a.Name)
	v.Check(name != "", "name", "must be provided")
	v.Check(len(name) <= 100, "name", "must not be more than 100 characters")
}

type ConnectaAgentModel struct {
	DB *sql.DB
}

func (m ConnectaAgentModel) Insert(a *ConnectaAgent) error {
	a.Name = strings.TrimSpace(a.Name)
	query := `
		INSERT INTO connecta_agents (name, created_by)
		VALUES ($1, $2)
		RETURNING id, created_at`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, a.Name, a.CreatedBy).Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		if err.Error() == `pq: duplicate key value violates unique constraint "connecta_agents_name_key"` {
			return ErrDuplicateConnectaAgent
		}
		return err
	}
	return nil
}

func (m ConnectaAgentModel) GetAll() ([]*ConnectaAgent, error) {
	query := `
		SELECT id, created_at, name, created_by
		FROM connecta_agents
		ORDER BY name ASC`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []*ConnectaAgent
	for rows.Next() {
		var a ConnectaAgent
		var createdBy sql.NullInt64
		if err := rows.Scan(&a.ID, &a.CreatedAt, &a.Name, &createdBy); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			a.CreatedBy = &createdBy.Int64
		}
		agents = append(agents, &a)
	}
	return agents, rows.Err()
}

func (m ConnectaAgentModel) GetByName(name string) (*ConnectaAgent, error) {
	query := `
		SELECT id, created_at, name, created_by
		FROM connecta_agents
		WHERE name = $1`

	var a ConnectaAgent
	var createdBy sql.NullInt64
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, strings.TrimSpace(name)).Scan(
		&a.ID, &a.CreatedAt, &a.Name, &createdBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	if createdBy.Valid {
		a.CreatedBy = &createdBy.Int64
	}
	return &a, nil
}

func (m ConnectaAgentModel) Delete(id int64) error {
	query := `DELETE FROM connecta_agents WHERE id = $1`

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
