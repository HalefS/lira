DROP INDEX IF EXISTS consumable_stock_movements_created_idx;
DROP INDEX IF EXISTS consumable_stock_movements_item_idx;
DROP TABLE IF EXISTS consumable_stock_movements;

ALTER TABLE consumable_items
    DROP COLUMN IF EXISTS reorder_level,
    DROP COLUMN IF EXISTS stock;
