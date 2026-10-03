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
	// Stock is how many the hotel has. It is maintained by hand and by the
	// app as consumables are recorded against issues, and is allowed to go
	// negative so that using more than was counted stays visible instead of
	// being silently clamped. ReorderLevel is the point at which the item is
	// flagged as running low; it warns, it does not block anything.
	Stock        int    `json:"stock"`
	ReorderLevel int    `json:"reorder_level"`
	LowStock     bool   `json:"low_stock"`
	CreatedBy    *int64 `json:"created_by,omitempty"`
}

// Reasons a stock movement can be recorded. The first two are deliberate
// changes by a person; the last two are the app following a consumable being
// recorded on an issue, or that recording being taken back off.
const (
	StockReasonAdded   = "added"
	StockReasonSet     = "set"
	StockReasonUsed    = "used"
	StockReasonRemoved = "removed"
)

// StockMovement is one entry in an item's inventory history.
type StockMovement struct {
	ID           int64     `json:"id"`
	CreatedAt    time.Time `json:"created_at"`
	ItemID       *int64    `json:"item_id"`
	Item         string    `json:"item"`
	Change       int       `json:"change"`
	BalanceAfter int       `json:"balance_after"`
	Reason       string    `json:"reason"`
	Note         string    `json:"note"`
	IssueID      *int64    `json:"issue_id"`
	LoggedByName string    `json:"logged_by_name"`
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

// consumableItemColumns is shared by every read of a catalog item so the scan
// order below can never drift from the select list.
//
// low_stock is derived rather than stored: a stored flag could disagree with
// the two numbers it comes from. A reorder level of zero means "this item is
// ordered on demand", so it is never flagged — without that, a brand new item
// at stock 0 with the default level of 0 would immediately read as low.
const consumableItemColumns = `
	id, created_at, name, icon, stock, reorder_level,
	reorder_level > 0 AND stock <= reorder_level, created_by`

func scanConsumableItem(scan func(dest ...any) error) (*ConsumableItem, error) {
	var ci ConsumableItem
	var createdBy sql.NullInt64
	if err := scan(&ci.ID, &ci.CreatedAt, &ci.Name, &ci.Icon, &ci.Stock, &ci.ReorderLevel, &ci.LowStock, &createdBy); err != nil {
		return nil, err
	}
	if createdBy.Valid {
		ci.CreatedBy = &createdBy.Int64
	}
	return &ci, nil
}

// GetAll returns up to limit catalog items, plus the total in the catalog.
//
// name, id as a tiebreaker: two items sharing a name would otherwise be free to
// swap places between requests, and "load more" would show one of them twice.
func (m ConsumableItemModel) GetAll(limit int) ([]*ConsumableItem, int, error) {
	ctx, cancel := listContext()
	defer cancel()

	total, err := CountMatching(ctx, m.DB, `FROM consumable_items`, nil)
	if err != nil {
		return nil, 0, err
	}

	query := `SELECT ` + consumableItemColumns + `
		FROM consumable_items
		ORDER BY name ASC, id ASC
		LIMIT $1`

	rows, err := m.DB.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []*ConsumableItem{}
	for rows.Next() {
		ci, err := scanConsumableItem(rows.Scan)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, ci)
	}
	return items, total, rows.Err()
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
		RETURNING ` + consumableItemColumns

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ci, err := scanConsumableItem(m.DB.QueryRowContext(ctx, query, icon, id).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return ci, nil
}

// UpdateReorderLevel sets the count at or below which an item is flagged as
// running low. Zero means "never flag it", which is the right setting for
// things that are simply ordered on demand.
func (m ConsumableItemModel) UpdateReorderLevel(id int64, level int) (*ConsumableItem, error) {
	query := `
		UPDATE consumable_items
		SET reorder_level = $1
		WHERE id = $2
		RETURNING ` + consumableItemColumns

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ci, err := scanConsumableItem(m.DB.QueryRowContext(ctx, query, level, id).Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}
	return ci, nil
}

// AdjustStock changes an item's stock by hand and records the movement.
// change is signed and relative; setTo is an absolute counted figure. Passing
// both, or a change of zero (which the movements table rejects, since a
// movement that moves nothing is a mistake rather than a movement), is an
// error.
func (m ConsumableItemModel) AdjustStock(id int64, change *int, setTo *int, note string, loggedBy *int64) (*ConsumableItem, error) {
	if (change == nil) == (setTo == nil) {
		return nil, errors.New("provide exactly one of change or set_to")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var current int
	if err := tx.QueryRowContext(ctx, `SELECT stock FROM consumable_items WHERE id = $1 FOR UPDATE`, id).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRecordNotFound
		}
		return nil, err
	}

	delta := 0
	reason := StockReasonAdded
	switch {
	case setTo != nil:
		delta = *setTo - current
		reason = StockReasonSet
	default:
		delta = *change
	}
	if delta == 0 {
		return nil, errors.New("this would not change the stock")
	}

	var ci ConsumableItem
	if err := tx.QueryRowContext(ctx, `
		UPDATE consumable_items
		SET stock = stock + $1
		WHERE id = $2
		RETURNING `+consumableItemColumns, delta, id).
		Scan(&ci.ID, &ci.CreatedAt, &ci.Name, &ci.Icon, &ci.Stock, &ci.ReorderLevel, &ci.LowStock, &ci.CreatedBy); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO consumable_stock_movements
			(item_id, item, change, balance_after, reason, note, logged_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ci.ID, ci.Name, delta, ci.Stock, reason, note, loggedBy,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ci, nil
}

// StockMovements returns an item's inventory history, newest first. Passing 0
// as itemID returns the movements for every item.
func (m ConsumableItemModel) StockMovements(itemID int64, limit int) ([]*StockMovement, error) {
	query := `
		SELECT s.id, s.created_at, s.item_id, s.item, s.change, s.balance_after,
		       s.reason, s.note, s.issue_id, COALESCE(u.name, '')
		FROM consumable_stock_movements s
		LEFT JOIN users u ON u.id = s.logged_by
		WHERE ($1 = 0 OR s.item_id = $1)
		ORDER BY s.created_at DESC, s.id DESC
		LIMIT $2`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query, itemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*StockMovement{}
	for rows.Next() {
		var s StockMovement
		var itemID, issueID sql.NullInt64
		if err := rows.Scan(&s.ID, &s.CreatedAt, &itemID, &s.Item, &s.Change,
			&s.BalanceAfter, &s.Reason, &s.Note, &issueID, &s.LoggedByName); err != nil {
			return nil, err
		}
		if itemID.Valid {
			s.ItemID = &itemID.Int64
		}
		if issueID.Valid {
			s.IssueID = &issueID.Int64
		}
		out = append(out, &s)
	}
	return out, rows.Err()
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
