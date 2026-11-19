-- A preset is a manager-authored answer to "what do we usually do for this kind
-- of issue?". It belongs to one issue type, so a Door preset can never be applied
-- to an Internet issue, and it carries the three fields that are genuinely
-- repetitive: the problem description, the resolution, and the consumables.
--
-- Everything else on an issue -- location, times, status, photos, TV swaps and
-- third-party support requests -- is specific to the incident and is deliberately
-- not part of a preset. Applying one replaces only those three fields.
CREATE TABLE IF NOT EXISTS issue_presets (
    id            bigserial PRIMARY KEY,
    issue_type_id bigint NOT NULL REFERENCES issue_types(id) ON DELETE CASCADE,
    name          citext NOT NULL,
    problem       text NOT NULL,
    resolution    text NOT NULL DEFAULT '',
    created_by    bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at    timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_at    timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    UNIQUE (issue_type_id, name)
);

CREATE INDEX IF NOT EXISTS issue_presets_issue_type_id_idx ON issue_presets (issue_type_id);

-- Consumables are rows, not a text blob, so stock and reporting can still see
-- which catalogue item a preset uses. Deleting either side removes the link: a
-- preset with a retired item is worse than a preset with one fewer item.
CREATE TABLE IF NOT EXISTS issue_preset_consumables (
    preset_id bigint NOT NULL REFERENCES issue_presets(id) ON DELETE CASCADE,
    item_id   bigint NOT NULL REFERENCES consumable_items(id) ON DELETE CASCADE,
    quantity  integer NOT NULL CHECK (quantity BETWEEN 1 AND 999),
    PRIMARY KEY (preset_id, item_id)
);

CREATE INDEX IF NOT EXISTS issue_preset_consumables_item_id_idx ON issue_preset_consumables (item_id);
