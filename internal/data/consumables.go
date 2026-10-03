package data

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/HalefS/lira/internal/validator"
	"github.com/lib/pq"
)

const (
	maxConsumableQuantity  = 999
	maxConsumablesPerIssue = 50
)

// Consumable is one line of consumable usage: "2x AA Batteries were used on
// this issue". It is what the Consumables page lists.
type Consumable struct {
	ID       int64  `json:"id"`
	IssueID  int64  `json:"issue_id"`
	ItemID   *int64 `json:"item_id"` // nil once the item has been removed from the catalog
	Item     string `json:"item"`
	Icon     string `json:"icon"`
	Quantity int    `json:"quantity"`
	// Mode and Location come from the issue the consumable was used on.
	Mode         string    `json:"mode"`
	Location     string    `json:"location"`
	LoggedBy     int64     `json:"logged_by"`
	LoggedByName string    `json:"logged_by_name"`
	CreatedAt    time.Time `json:"created_at"`
}

// IssueConsumable is the compact form attached to each issue, so the issue
// lists can show which consumables were used without a second request.
type IssueConsumable struct {
	ItemID   *int64 `json:"item_id"`
	Item     string `json:"item"`
	Icon     string `json:"icon"`
	Quantity int    `json:"quantity"`
}

// ConsumableTotal is the running total used for one item.
type ConsumableTotal struct {
	Item     string `json:"item"`
	Icon     string `json:"icon"`
	Quantity int    `json:"quantity"`
}

// ConsumableUse is one consumable selected for an issue, as sent by the
// client when an issue is created or edited.
type ConsumableUse struct {
	ItemID   int64 `json:"item_id"`
	Quantity int   `json:"quantity"`
}

// ValidateConsumableUses checks the shape of a selection. It does not check
// that the items exist — see ConsumableItemModel.CountByIDs for that.
func ValidateConsumableUses(v *validator.Validator, uses []ConsumableUse) {
	v.Check(len(uses) <= maxConsumablesPerIssue, "consumables", "must not contain more than 50 items")

	seen := make(map[int64]bool, len(uses))
	for _, u := range uses {
		v.Check(u.ItemID >= 1, "consumables", "each item must reference a valid consumable")
		v.Check(u.Quantity >= 1 && u.Quantity <= maxConsumableQuantity, "consumables", "each quantity must be between 1 and 999")
		v.Check(!seen[u.ItemID], "consumables", "the same consumable can only be listed once")
		seen[u.ItemID] = true
	}
}

type ConsumableModel struct {
	DB *sql.DB
}

// SetForIssue makes the issue's consumables match the given selection:
// selected items are inserted (or have their quantity updated), and items that
// were previously recorded but are no longer selected are removed.
//
// It also keeps the inventory straight. Taking a consumable off an issue draws
// it down from the item's stock; adding one, or taking it off again, puts it
// back. Each change writes a movement so a count can be explained later. All
// of it happens in the single transaction below, so the stock can never be
// left out of step with the usage rows.
//
// Rows whose catalog entry has since been deleted (item_id IS NULL) are
// historical records the edit form can't show, so they are left untouched, and
// they never touch stock — the item they belonged to no longer exists.
// Existing rows keep their original logged_by and created_at; only new rows are
// attributed to loggedBy.
func (m ConsumableModel) SetForIssue(issueID int64, uses []ConsumableUse, loggedBy int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Must be non-nil: a nil slice would be sent as SQL NULL, and
	// "<> ALL(NULL)" matches no rows, so nothing would be removed.
	keep := make([]int64, 0, len(uses))
	for _, u := range uses {
		keep = append(keep, u.ItemID)
	}

	// What this issue consumed before the edit, so the differences can be
	// turned into stock movements. Rows whose item left the catalog are
	// skipped: there is no stock left to move.
	before, err := quantitiesByItem(ctx, tx, issueID)
	if err != nil {
		return err
	}

	// Hand the stock back for everything being removed, recording the movement
	// before the rows disappear.
	for itemID, qty := range before {
		if !containsID(keep, itemID) {
			if err := applyStock(ctx, tx, itemID, qty, StockReasonRemoved, issueID, loggedBy); err != nil {
				return err
			}
		}
	}

	_, err = tx.ExecContext(ctx, `
		DELETE FROM consumables
		WHERE issue_id = $1 AND item_id IS NOT NULL AND item_id <> ALL($2::bigint[])`,
		issueID, pq.Array(keep),
	)
	if err != nil {
		return err
	}

	for _, u := range uses {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO consumables (issue_id, item_id, item, quantity, logged_by)
			SELECT $1::bigint, ci.id, ci.name, $3::integer, $4::bigint
			FROM consumable_items ci
			WHERE ci.id = $2::bigint
			ON CONFLICT (issue_id, item) DO UPDATE
			SET item_id = EXCLUDED.item_id, quantity = EXCLUDED.quantity`,
			issueID, u.ItemID, u.Quantity, loggedBy,
		)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			// The catalog entry doesn't exist (e.g. deleted since the form loaded).
			return ErrRecordNotFound
		}

		// Draw down only the difference. Re-saving an issue without touching
		// the quantity must not take the stock down a second time.
		if delta := before[u.ItemID] - u.Quantity; delta != 0 {
			// delta is positive when fewer are now used than before (stock back
			// in), negative when more are used (stock drawn down).
			if err := applyStock(ctx, tx, u.ItemID, delta, StockReasonUsed, issueID, loggedBy); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// quantitiesByItem returns the consumable quantities currently recorded against
// an issue, keyed by catalog item. Rows whose item has been deleted from the
// catalog are left out, because there is no stock left to move for them.
func quantitiesByItem(ctx context.Context, tx *sql.Tx, issueID int64) (map[int64]int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT item_id, quantity
		FROM consumables
		WHERE issue_id = $1 AND item_id IS NOT NULL`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]int)
	for rows.Next() {
		var itemID int64
		var qty int
		if err := rows.Scan(&itemID, &qty); err != nil {
			return nil, err
		}
		out[itemID] = qty
	}
	return out, rows.Err()
}

// applyStock moves an item's stock by a signed amount and records the movement
// that explains it. The row is locked while it is updated so two concurrent
// edits cannot both read the same balance and lose one of the changes.
//
// A missing item is not an error here: it means the catalog entry was deleted
// between the form loading and the save, and there is no stock to move.
func applyStock(ctx context.Context, tx *sql.Tx, itemID int64, delta int, reason string, issueID int64, loggedBy int64) error {
	row := tx.QueryRowContext(ctx, `
		UPDATE consumable_items
		SET stock = stock + $1
		WHERE id = $2
		RETURNING name, stock`, delta, itemID)

	var name string
	var balance int
	if err := row.Scan(&name, &balance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	var issue interface{} = issueID
	_, err := tx.ExecContext(ctx, `
		INSERT INTO consumable_stock_movements
			(item_id, item, change, balance_after, reason, issue_id, logged_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		itemID, name, delta, balance, reason, issue, loggedBy)
	return err
}

func containsID(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// loadIssueConsumables fills in Consumables on each of the given issues with a
// single query. Every issue ends up with a non-nil slice, so the JSON is always
// an array.
func loadIssueConsumables(ctx context.Context, db *sql.DB, issues []*Issue) error {
	if len(issues) == 0 {
		return nil
	}

	byID := make(map[int64]*Issue, len(issues))
	ids := make([]int64, 0, len(issues))
	for _, is := range issues {
		is.Consumables = []*IssueConsumable{}
		byID[is.ID] = is
		ids = append(ids, is.ID)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT c.issue_id, c.item_id, c.item, COALESCE(ci.icon, 'box'), c.quantity
		FROM consumables c
		LEFT JOIN consumable_items ci ON ci.id = c.item_id
		WHERE c.issue_id = ANY($1::bigint[])
		ORDER BY c.id`,
		pq.Array(ids),
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var issueID int64
		var itemID sql.NullInt64
		var ic IssueConsumable
		if err := rows.Scan(&issueID, &itemID, &ic.Item, &ic.Icon, &ic.Quantity); err != nil {
			return err
		}
		if itemID.Valid {
			id := itemID.Int64
			ic.ItemID = &id
		}
		if is, ok := byID[issueID]; ok {
			is.Consumables = append(is.Consumables, &ic)
		}
	}
	return rows.Err()
}

// GetAll returns up to limit consumable usage rows, optionally filtered to a
// single calendar date ("YYYY-MM-DD"), newest first, plus the total that matched
// the filter.
func (m ConsumableModel) GetAll(date string, limit int) ([]*Consumable, int, error) {
	fromWhere := `
		FROM consumables c
		INNER JOIN issues i ON i.id = c.issue_id
		INNER JOIN users u ON c.logged_by = u.id
		LEFT JOIN consumable_items ci ON ci.id = c.item_id
		WHERE ($1 = '' OR c.created_at::date = $1::date)`

	ctx, cancel := listContext()
	defer cancel()

	total, err := CountMatching(ctx, m.DB, fromWhere, []any{date})
	if err != nil {
		return nil, 0, err
	}

	query := `
		SELECT c.id, c.issue_id, c.item_id, c.item, COALESCE(ci.icon, 'box'), c.quantity,
		       i.mode, i.location, c.logged_by, u.name, c.created_at
		` + fromWhere + `
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT $2`

	rows, err := m.DB.QueryContext(ctx, query, date, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []*Consumable{}
	for rows.Next() {
		var c Consumable
		var itemID sql.NullInt64
		if err := rows.Scan(
			&c.ID, &c.IssueID, &itemID, &c.Item, &c.Icon, &c.Quantity,
			&c.Mode, &c.Location, &c.LoggedBy, &c.LoggedByName, &c.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		if itemID.Valid {
			id := itemID.Int64
			c.ItemID = &id
		}
		out = append(out, &c)
	}
	return out, total, rows.Err()
}

// SummaryByItem returns the total quantity used per item — e.g. how many AA
// Batteries, Remote Controls and Phones have been used in total — most used
// first.
func (m ConsumableModel) SummaryByItem() ([]*ConsumableTotal, error) {
	query := `
		SELECT c.item, COALESCE(MAX(ci.icon), 'box'), SUM(c.quantity)
		FROM consumables c
		LEFT JOIN consumable_items ci ON ci.id = c.item_id
		GROUP BY c.item
		ORDER BY SUM(c.quantity) DESC, c.item ASC`

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := m.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ConsumableTotal
	for rows.Next() {
		var t ConsumableTotal
		if err := rows.Scan(&t.Item, &t.Icon, &t.Quantity); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}
