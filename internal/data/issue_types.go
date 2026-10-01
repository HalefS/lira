package data

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/HalefS/lira/internal/validator"
)

type IssueType struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	// TracksSwaps marks a type whose issues can involve moving equipment
	// between rooms — a TV taken from another room because the room's own set
	// is unusable and there is no spare in stock. When it is set, the issue
	// form offers to record the swap.
	//
	// This is stored on the type rather than inferred from its name so that
	// renaming the type cannot silently stop the prompt appearing, and so the
	// same mechanism can be switched on for other equipment later.
	TracksSwaps bool   `json:"tracks_swaps"`
	CreatedBy   *int64 `json:"created_by,omitempty"`
}

// issueTypeColumns is shared by every read of an issue type so the scan order
// below can never drift from the select list.
const issueTypeColumns = `id, created_at, name, color, tracks_swaps, created_by`

func scanIssueType(scan func(dest ...any) error) (*IssueType, error) {
	var it IssueType
	var createdBy sql.NullInt64
	if err := scan(&it.ID, &it.CreatedAt, &it.Name, &it.Color, &it.TracksSwaps, &createdBy); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		it.CreatedBy = &createdBy.Int64
	}
	return &it, nil
}

func ValidateIssueType(v *validator.Validator, it *IssueType) {
	name := strings.TrimSpace(it.Name)
	v.Check(name != "", "name", "must be provided")
	v.Check(len(name) <= 50, "name", "must not be more than 50 characters")
}

var hexColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// ValidateColorHex checks a "#RRGGBB" hex color string, matching exactly
// what a native <input type="color"> element produces.
func ValidateColorHex(v *validator.Validator, field, value string) {
	v.Check(hexColorPattern.MatchString(value), field, "must be a hex color in #RRGGBB format")
}

// issueTypeColorPalette is the fixed, ordered set of colors automatically
// handed out to a newly created issue type. Kept deliberately larger than
// any realistic number of issue types so uniqueness holds in practice.
// A manager can always override any type's color afterwards via the color
// picker in the admin panel, which stores whatever hex value they choose.
var issueTypeColorPalette = []string{
	"#3730A3", "#1D9E75", "#D88A18", "#6B21A8",
	"#075985", "#9F1239", "#334155", "#9A3412",
	"#86198F", "#991B1B", "#854D0E", "#4D7C0F",
	"#065F46", "#155E75", "#1E40AF", "#5B21B6",
	"#BE185D", "#78502B",
}

type IssueTypeModel struct {
	DB *sql.DB
}

// NextAvailableColor returns the first palette color not already assigned
// to an existing issue type, guaranteeing every type gets a unique color
// until the palette is exhausted — past that point it reuses whichever
// color is currently least common, rather than leaving a type uncolored.
func (m IssueTypeModel) NextAvailableColor() (string, error) {
	query := `SELECT color FROM issue_types`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	used := make(map[string]int)
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return "", err
		}
		used[c]++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	for _, c := range issueTypeColorPalette {
		if used[c] == 0 {
			return c, nil
		}
	}

	// Palette exhausted — fall back to the least-used color.
	best := issueTypeColorPalette[0]
	for _, c := range issueTypeColorPalette {
		if used[c] < used[best] {
			best = c
		}
	}
	return best, nil
}

func (m IssueTypeModel) Insert(it *IssueType) error {
	it.Name = strings.TrimSpace(it.Name)
	query := `
		INSERT INTO issue_types (name, color, tracks_swaps, created_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, it.Name, it.Color, it.TracksSwaps, it.CreatedBy).
		Scan(&it.ID, &it.CreatedAt)
	if err != nil {
		if err.Error() == `pq: duplicate key value violates unique constraint "issue_types_name_key"` {
			return ErrDuplicateIssueType
		}
		return err
	}
	return nil
}

func (m IssueTypeModel) GetAll() ([]*IssueType, error) {
	query := `SELECT ` + issueTypeColumns + `
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
		it, err := scanIssueType(rows.Scan)
		if err != nil {
			return nil, err
		}
		types = append(types, it)
	}
	return types, rows.Err()
}

func (m IssueTypeModel) GetByName(name string) (*IssueType, error) {
	query := `SELECT ` + issueTypeColumns + `
		FROM issue_types
		WHERE name = $1`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	it, err := scanIssueType(m.DB.QueryRowContext(ctx, query, strings.TrimSpace(name)).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return it, nil
}

// UpdateColor sets a specific issue type's color to a manager-chosen hex
// value, permanently overriding whatever it was auto-assigned at creation.
func (m IssueTypeModel) UpdateColor(id int64, hex string) (*IssueType, error) {
	query := `
		UPDATE issue_types
		SET color = $1
		WHERE id = $2
		RETURNING ` + issueTypeColumns

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	it, err := scanIssueType(m.DB.QueryRowContext(ctx, query, hex, id).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return it, nil
}

// UpdateTracksSwaps turns the "can involve moving equipment between rooms"
// behaviour on or off for one issue type.
func (m IssueTypeModel) UpdateTracksSwaps(id int64, tracks bool) (*IssueType, error) {
	query := `
		UPDATE issue_types
		SET tracks_swaps = $1
		WHERE id = $2
		RETURNING ` + issueTypeColumns

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	it, err := scanIssueType(m.DB.QueryRowContext(ctx, query, tracks, id).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return it, nil
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
