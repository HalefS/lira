package data

import (
	"context"
	"database/sql"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

type Settings struct {
	DuplicateWindowHours int `json:"duplicate_window_hours"`
	// LCUWindowDays is how many calendar days a newly added LCU test unit is
	// trialled for. Units already on trial keep the window they were given.
	LCUWindowDays int       `json:"lcu_window_days"`
	UpdatedAt     time.Time `json:"updated_at"`
	UpdatedBy     *int64    `json:"updated_by,omitempty"`
}

func ValidateSettings(v *validator.Validator, s *Settings) {
	v.Check(s.DuplicateWindowHours > 0, "duplicate_window_hours", "must be greater than zero")
	v.Check(s.DuplicateWindowHours <= 24*30, "duplicate_window_hours", "must not be more than 720 hours (30 days)")
	v.Check(s.LCUWindowDays >= MinLCUWindowDays, "lcu_window_days",
		"must be at least 1 day")
	v.Check(s.LCUWindowDays <= MaxLCUWindowDays, "lcu_window_days",
		"must not be more than 60 days")
}

type SettingsModel struct {
	DB *sql.DB
}

// Get returns the single application settings row. If the row is somehow
// missing (e.g. the seed insert didn't run on an older deployment), it
// falls back to sane defaults rather than erroring.
func (m SettingsModel) Get() (*Settings, error) {
	query := `
		SELECT duplicate_window_hours, lcu_window_days, updated_at, updated_by
		FROM app_settings
		WHERE id = 1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var s Settings
	var updatedBy sql.NullInt64
	err := m.DB.QueryRowContext(ctx, query).Scan(&s.DuplicateWindowHours,
		&s.LCUWindowDays, &s.UpdatedAt, &updatedBy)
	if err != nil {
		if err == sql.ErrNoRows {
			return &Settings{
				DuplicateWindowHours: 24,
				LCUWindowDays:        DefaultLCUWindowDays,
			}, nil
		}
		return nil, err
	}
	// A row predating the column, or one edited straight in the database, must
	// still hand callers a window they can use.
	s.LCUWindowDays = ClampLCUWindowDays(s.LCUWindowDays)
	if updatedBy.Valid {
		s.UpdatedBy = &updatedBy.Int64
	}
	return &s, nil
}

// Update upserts the singleton settings row.
func (m SettingsModel) Update(hours, lcuWindowDays int, updatedBy int64) (*Settings, error) {
	query := `
		INSERT INTO app_settings (id, duplicate_window_hours, lcu_window_days, updated_at, updated_by)
		VALUES (1, $1, $2, NOW(), $3)
		ON CONFLICT (id) DO UPDATE
		SET duplicate_window_hours = EXCLUDED.duplicate_window_hours,
		    lcu_window_days        = EXCLUDED.lcu_window_days,
		    updated_at             = NOW(),
		    updated_by             = EXCLUDED.updated_by
		RETURNING duplicate_window_hours, lcu_window_days, updated_at, updated_by`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var s Settings
	var ub sql.NullInt64
	err := m.DB.QueryRowContext(ctx, query, hours, lcuWindowDays, updatedBy).Scan(
		&s.DuplicateWindowHours, &s.LCUWindowDays, &s.UpdatedAt, &ub)
	if err != nil {
		return nil, err
	}
	if ub.Valid {
		s.UpdatedBy = &ub.Int64
	}
	return &s, nil
}
