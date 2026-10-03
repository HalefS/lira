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
	LCUWindowDays int `json:"lcu_window_days"`
	// SLAMinutes is the resolution time, in minutes, an issue may take before it
	// is shown as having breached the service level agreement. A pointer because
	// nil and zero mean different things here: nil is no SLA configured, so
	// nothing is ever flagged, and zero would mark every issue that took any time
	// at all -- including a three-minute fix -- as a breach.
	SLAMinutes *int      `json:"sla_minutes"`
	UpdatedAt  time.Time `json:"updated_at"`
	UpdatedBy  *int64    `json:"updated_by,omitempty"`
}

// MaxSLAMinutes bounds the threshold so a typo cannot quietly disable the check
// by setting it to a number no issue will ever reach. Ten days is far beyond any
// plausible IT response target and still well inside int range.
const MaxSLAMinutes = 60 * 24 * 10

func ValidateSettings(v *validator.Validator, s *Settings) {
	v.Check(s.DuplicateWindowHours > 0, "duplicate_window_hours", "must be greater than zero")
	v.Check(s.DuplicateWindowHours <= 24*30, "duplicate_window_hours", "must not be more than 720 hours (30 days)")
	v.Check(s.LCUWindowDays >= MinLCUWindowDays, "lcu_window_days",
		"must be at least 1 day")
	v.Check(s.LCUWindowDays <= MaxLCUWindowDays, "lcu_window_days",
		"must not be more than 60 days")
	// Only checked when set: leaving the field blank is how the SLA is turned
	// off, and that has to be a valid thing to save.
	if s.SLAMinutes != nil {
		v.Check(*s.SLAMinutes > 0, "sla_minutes", "must be greater than zero")
		v.Check(*s.SLAMinutes <= MaxSLAMinutes, "sla_minutes",
			"must not be more than 14400 minutes (10 days)")
	}
}

type SettingsModel struct {
	DB *sql.DB
}

// Get returns the single application settings row. If the row is somehow
// missing (e.g. the seed insert didn't run on an older deployment), it
// falls back to sane defaults rather than erroring.
func (m SettingsModel) Get() (*Settings, error) {
	query := `
		SELECT duplicate_window_hours, lcu_window_days, sla_minutes, updated_at, updated_by
		FROM app_settings
		WHERE id = 1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var s Settings
	var sla sql.NullInt64
	var updatedBy sql.NullInt64
	err := m.DB.QueryRowContext(ctx, query).Scan(&s.DuplicateWindowHours,
		&s.LCUWindowDays, &sla, &s.UpdatedAt, &updatedBy)
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
	if sla.Valid {
		minutes := int(sla.Int64)
		s.SLAMinutes = &minutes
	}
	if updatedBy.Valid {
		s.UpdatedBy = &updatedBy.Int64
	}
	return &s, nil
}

// Update upserts the singleton settings row.
//
// slaMinutes is a pointer for the same reason the column is nullable: nil clears
// the SLA, and that is a distinct outcome from setting it to some number. A plain
// int here could not tell "leave it alone" from "set it to zero".
func (m SettingsModel) Update(hours, lcuWindowDays int, slaMinutes *int, updatedBy int64) (*Settings, error) {
	query := `
		INSERT INTO app_settings (id, duplicate_window_hours, lcu_window_days, sla_minutes, updated_at, updated_by)
		VALUES (1, $1, $2, $3, NOW(), $4)
		ON CONFLICT (id) DO UPDATE
		SET duplicate_window_hours = EXCLUDED.duplicate_window_hours,
		    lcu_window_days        = EXCLUDED.lcu_window_days,
		    sla_minutes            = EXCLUDED.sla_minutes,
		    updated_at             = NOW(),
		    updated_by             = EXCLUDED.updated_by
		RETURNING duplicate_window_hours, lcu_window_days, sla_minutes, updated_at, updated_by`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var s Settings
	var sla sql.NullInt64
	var ub sql.NullInt64
	err := m.DB.QueryRowContext(ctx, query, hours, lcuWindowDays, slaMinutes, updatedBy).Scan(
		&s.DuplicateWindowHours, &s.LCUWindowDays, &sla, &s.UpdatedAt, &ub)
	if err != nil {
		return nil, err
	}
	if sla.Valid {
		minutes := int(sla.Int64)
		s.SLAMinutes = &minutes
	}
	if ub.Valid {
		s.UpdatedBy = &ub.Int64
	}
	return &s, nil
}
