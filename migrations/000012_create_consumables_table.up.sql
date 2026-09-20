-- Tracks inventory consumed while resolving an issue (batteries, remote
-- controls, phones, etc). At most one consumable record per issue — which
-- item it is is derived from the issue's type (see consumableItemForType
-- in cmd/api/consumables.go), so toggling it on/off in the issue form
-- simply creates or removes this one row.
CREATE TABLE IF NOT EXISTS consumables (
    id         bigserial PRIMARY KEY,
    issue_id   bigint UNIQUE NOT NULL REFERENCES issues ON DELETE CASCADE,
    item       text NOT NULL,
    mode       text NOT NULL,
    location   text NOT NULL,
    logged_by  bigint REFERENCES users ON DELETE SET NULL,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS consumables_item_idx ON consumables (item);
CREATE INDEX IF NOT EXISTS consumables_created_at_idx ON consumables (created_at);
