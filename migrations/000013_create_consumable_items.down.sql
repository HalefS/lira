ALTER TABLE consumables DROP CONSTRAINT IF EXISTS consumables_issue_id_item_key;
DROP INDEX IF EXISTS consumables_item_id_idx;

-- The old schema allowed a single consumable per issue: keep the earliest.
DELETE FROM consumables a
USING consumables b
WHERE a.issue_id = b.issue_id AND a.id > b.id;

ALTER TABLE consumables
    ADD COLUMN mode     text,
    ADD COLUMN location text;

UPDATE consumables c
SET mode = i.mode, location = i.location
FROM issues i
WHERE i.id = c.issue_id;

ALTER TABLE consumables
    ALTER COLUMN mode     SET NOT NULL,
    ALTER COLUMN location SET NOT NULL;

ALTER TABLE consumables ADD CONSTRAINT consumables_issue_id_key UNIQUE (issue_id);

ALTER TABLE consumables
    DROP COLUMN IF EXISTS item_id,
    DROP COLUMN IF EXISTS quantity;

DROP TABLE IF EXISTS consumable_items;
