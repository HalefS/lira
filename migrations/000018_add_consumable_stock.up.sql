-- Inventory: how many of each consumable the hotel actually has, and a record
-- of every change to that number.
--
-- `stock` is the current count. It is decremented automatically when a
-- consumable is recorded against an issue, and incremented again if that
-- recording is edited away or the issue is deleted, so the count always
-- reflects reality as far as the app knows it.
--
-- The count is allowed to go NEGATIVE on purpose. A technician who has
-- physically taken three batteries has done the work, and refusing to record
-- it because the cupboard count was wrong helps nobody — the negative number
-- is the alarm that says "recount this", and it stays visible until it is
-- corrected with a stock adjustment.
--
-- `reorder_level` is the per-item point at which the item is flagged as
-- running low. It is a warning only; nothing is blocked by it.
ALTER TABLE consumable_items
    ADD COLUMN IF NOT EXISTS stock         integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reorder_level integer NOT NULL DEFAULT 0 CHECK (reorder_level >= 0);

-- Every change to an item's stock. `change` is signed (+ added, − used) and
-- `balance_after` is the resulting count, written in the same transaction as
-- the stock update so the two can never disagree.
--
-- The item name is snapshotted the same way `consumables.item` is, so the
-- history survives an item being dropped from the catalog. `issue_id` is
-- filled when the movement came from a consumable recorded on an issue, and
-- goes NULL if that issue is later deleted — the movement itself stays,
-- because the stock it returned is part of the item's history.
CREATE TABLE IF NOT EXISTS consumable_stock_movements (
    id            bigserial PRIMARY KEY,
    created_at    timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    item_id       bigint REFERENCES consumable_items ON DELETE SET NULL,
    item          text NOT NULL,
    change        integer NOT NULL CHECK (change <> 0),
    balance_after integer NOT NULL,
    -- added: stock was put in by hand
    -- set:   stock was corrected to a counted figure
    -- used:   a consumable was recorded on an issue
    -- removed: that recording was edited away, or its issue was deleted
    reason        text NOT NULL CHECK (reason IN ('added', 'set', 'used', 'removed')),
    note          text NOT NULL DEFAULT '',
    issue_id      bigint REFERENCES issues ON DELETE SET NULL,
    logged_by     bigint REFERENCES users ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS consumable_stock_movements_item_idx    ON consumable_stock_movements (item_id, created_at DESC);
CREATE INDEX IF NOT EXISTS consumable_stock_movements_created_idx ON consumable_stock_movements (created_at DESC);
