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

CREATE TABLE IF NOT EXISTS issue_preset_consumables (
    preset_id bigint NOT NULL REFERENCES issue_presets(id) ON DELETE CASCADE,
    item_id   bigint NOT NULL REFERENCES consumable_items(id) ON DELETE CASCADE,
    quantity  integer NOT NULL CHECK (quantity BETWEEN 1 AND 999),
    PRIMARY KEY (preset_id, item_id)
);

CREATE INDEX IF NOT EXISTS issue_preset_consumables_item_id_idx ON issue_preset_consumables (item_id);
