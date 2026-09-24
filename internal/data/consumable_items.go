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

// ConsumableItem is an entry in the catalog of consumables that technicians
// can pick from when logging an issue. Managers maintain the catalog from the
// admin panel.
type ConsumableItem struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Name      string    `json:"name"`
	Icon      string    `json:"icon"`
	CreatedBy *int64    `json:"created_by,omitempty"`
}

// DefaultConsumableIcon is used when a consumable is created without an icon.
const DefaultConsumableIcon = "box"

// ConsumableIcons is the set of icon keys a consumable can use. Every key has
// a matching drawing in the frontend (CONSUMABLE_ICONS in ui/index.html), so
// the two lists must be kept in sync when adding an icon.
var ConsumableIcons = []string{
	"battery-aa", "battery-aaa", "battery-9v", "battery-coin",
	"phone", "mobile", "tablet", "remote",
	"tv", "monitor", "hdmi", "ethernet",
	"charger", "power-strip", "bulb", "key-card",
	"lock", "router", "usb", "mouse",
	"keyboard", "headset", "printer", "box",
}

var consumableIconSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(ConsumableIcons))
	for _, icon := range ConsumableIcons {
		set[icon] = struct{}{}
	}
	return set
}()

func IsConsumableIcon(icon string) bool {
	_, ok := consumableIconSet[icon]
	return ok
}

func ValidateConsumableIcon(v *validator.Validator, field, icon string) {
	v.Check(IsConsumableIcon(icon), field, "must be one of the supported icons")
}

func ValidateConsumableItem(v *validator.Validator, ci *ConsumableItem) {
	name := strings.TrimSpace(ci.Name)
	v.Check(name != "", "name", "must be provided")
	v.Check(len(name) <= 50, "name", "must not be more than 50 characters")
	ValidateConsumableIcon(v, "icon", ci.Icon)
}

type ConsumableItemModel struct {
	DB *sql.DB
}

func (m ConsumableItemModel) Insert(ci *ConsumableItem) error {
	ci.Name = strings.TrimSpace(ci.Name)
	query := `
		INSERT INTO consumable_items (name, icon, created_by)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := m.DB.QueryRowContext(ctx, query, ci.Name, ci.Icon, ci.CreatedBy).Scan(&ci.ID, &ci.CreatedAt)
	if err != nil {
		var pqErr *pq.Error
		// 23505 = unique_violation (the name column is unique)
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return ErrDuplicateConsumableItem
		}
		return err
	}
	return nil
}

func (m ConsumableItemModel) GetAll() ([]*ConsumableItem, error) {
	query := `
		SELECT id, created_at, name, icon, created_by
		FROM consumable_items
		ORDER BY name ASC`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*ConsumableItem
	for rows.Next() {
		var ci ConsumableItem
		var createdBy sql.NullInt64
		if err := rows.Scan(&ci.ID, &ci.CreatedAt, &ci.Name, &ci.Icon, &createdBy); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			ci.CreatedBy = &createdBy.Int64
		}
		items = append(items, &ci)
	}
	return items, rows.Err()
}

// CountByIDs returns how many of the given ids exist in the catalog, so a
// caller can check that a whole selection is valid with a single query.
func (m ConsumableItemModel) CountByIDs(ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var n int
	err := m.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM consumable_items WHERE id = ANY($1::bigint[])`,
		pq.Array(ids),
	).Scan(&n)
	return n, err
}

func (m ConsumableItemModel) UpdateIcon(id int64, icon string) (*ConsumableItem, error) {
	query := `
		UPDATE consumable_items
		SET icon = $1
		WHERE id = $2
		RETURNING id, created_at, name, icon, created_by`

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var ci ConsumableItem
	var createdBy sql.NullInt64
	err := m.DB.QueryRowContext(ctx, query, icon, id).Scan(
		&ci.ID, &ci.CreatedAt, &ci.Name, &ci.Icon, &createdBy,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	if createdBy.Valid {
		ci.CreatedBy = &createdBy.Int64
	}
	return &ci, nil
}

// Delete removes an item from the catalog. Usage that was already recorded
// against it is kept (the usage rows keep the item's name).
func (m ConsumableItemModel) Delete(id int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := m.DB.ExecContext(ctx, `DELETE FROM consumable_items WHERE id = $1`, id)
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
