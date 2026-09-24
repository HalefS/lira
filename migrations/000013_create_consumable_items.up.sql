-- Catalog of consumables (AA batteries, remote controls, phones, ...) that
-- technicians can pick from when logging an issue. Managers maintain it from
-- the admin panel. `icon` is a key into the icon set drawn by the frontend
-- (see ConsumableIcons in internal/data/consumable_items.go).
CREATE TABLE IF NOT EXISTS consumable_items (
    id         bigserial PRIMARY KEY,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    name       citext UNIQUE NOT NULL,
    icon       text NOT NULL DEFAULT 'box',
    created_by bigint REFERENCES users ON DELETE SET NULL
);

INSERT INTO consumable_items (name, icon) VALUES
    ('AA Batteries',   'battery-aa'),
    ('AAA Batteries',  'battery-aaa'),
    ('Remote Control', 'remote'),
    ('Phone',          'phone')
ON CONFLICT (name) DO NOTHING;

-- The `consumables` table (created in 000012) records what was used on an
-- issue. It used to hold at most one row per issue, with the item derived from
-- the issue type. It now holds one row per (issue, item) with a quantity.
--
-- `item` stays as a snapshot of the name, and `item_id` links back to the
-- catalog. If a manager later deletes a catalog entry, item_id becomes NULL but
-- the usage history is kept.
ALTER TABLE consumables DROP CONSTRAINT IF EXISTS consumables_issue_id_key;

ALTER TABLE consumables
    ADD COLUMN IF NOT EXISTS item_id  bigint REFERENCES consumable_items ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS quantity integer NOT NULL DEFAULT 1 CHECK (quantity BETWEEN 1 AND 999);

-- Link the rows recorded by the old feature to their catalog entries.
UPDATE consumables c
SET item_id = ci.id
FROM consumable_items ci
WHERE ci.name = c.item::citext;

-- mode and location were copies of values that live on the issue itself. They
-- are read from the issue now, so they can no longer go stale.
ALTER TABLE consumables
    DROP COLUMN IF EXISTS mode,
    DROP COLUMN IF EXISTS location;

ALTER TABLE consumables ADD CONSTRAINT consumables_issue_id_item_key UNIQUE (issue_id, item);
CREATE INDEX IF NOT EXISTS consumables_item_id_idx ON consumables (item_id);
