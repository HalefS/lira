-- The room map: what each room is supposed to contain, and what is actually
-- there and working.
--
-- The inventory in `rooms` answers "does 1207 exist?". It deliberately holds
-- ranges rather than one row per room, because a hotel with three hundred rooms
-- should not be three hundred rows to maintain. The map needs the opposite
-- shape: one record per room, because a category and a checked state belong to
-- 1207 specifically and not to the run 1201-1212 it sits in. So the inventory
-- remains the source of which rooms exist, and `room_units` only holds a row for
-- a room somebody has actually classified or checked. A room with no row here is
-- simply unassigned, which is the honest state for a room nobody has visited.
--
-- Categories are created first. A category is a template -- "Standard: one TV and
-- one phone" -- and the quantities live in `room_category_equipment`. A room is
-- then given one category, and its own counts are recorded in `room_equipment`.
--
-- Counts, not booleans. A room with two TVs, one dead, is neither "has TVs" nor
-- "has no TVs": it is present = 2, working = 1. The working <= present check is
-- what stops a survey recording a phantom second working set, and it is enforced
-- in the database so no future endpoint can write around it.

CREATE TABLE IF NOT EXISTS room_categories (
    id         bigserial PRIMARY KEY,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    name       citext UNIQUE NOT NULL,
    -- The colour the map uses for this category. Stored as #RRGGBB, the same
    -- format the issue-type colour picker produces.
    color      text NOT NULL DEFAULT '#1769AA',
    created_by bigint REFERENCES users ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS equipment_types (
    id         bigserial PRIMARY KEY,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    name       citext UNIQUE NOT NULL,
    -- Which icon the map draws for this equipment. A short token, not a path:
    -- the UI owns the drawing, so an unknown token falls back to a box rather
    -- than rendering a broken image.
    icon       text NOT NULL DEFAULT 'box',
    created_by bigint REFERENCES users ON DELETE SET NULL
);

-- What a category expects. Quantity >= 1 because a row with zero would be a
-- piece of equipment the category does not actually contain, and the way to say
-- that is to remove the row.
CREATE TABLE IF NOT EXISTS room_category_equipment (
    category_id       bigint NOT NULL REFERENCES room_categories(id) ON DELETE CASCADE,
    equipment_type_id bigint NOT NULL REFERENCES equipment_types(id) ON DELETE CASCADE,
    quantity          integer NOT NULL CHECK (quantity >= 1),
    PRIMARY KEY (category_id, equipment_type_id)
);

-- One room's classification and survey state. room_number is the number as it
-- appears in the inventory; there is no foreign key because the inventory is a
-- range, not a table of rooms. Writes check Contains against the inventory.
--
-- category_id is ON DELETE SET NULL rather than CASCADE: deleting a category
-- must not delete the rooms classified with it, and must certainly not erase the
-- record of what equipment was found. The rooms become unassigned, which is a
-- visible problem a manager can fix, rather than silently disappearing.
CREATE TABLE IF NOT EXISTS room_units (
    room_number integer PRIMARY KEY,
    category_id bigint REFERENCES room_categories(id) ON DELETE SET NULL,
    -- NULL means "classified, but nobody has surveyed the equipment yet". That
    -- is a different answer from "surveyed and everything is fine", and the map
    -- shows the difference.
    surveyed_at timestamp(0) with time zone,
    notes       text NOT NULL DEFAULT '',
    updated_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_by  bigint REFERENCES users ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS room_equipment (
    room_number       integer NOT NULL REFERENCES room_units(room_number) ON DELETE CASCADE,
    equipment_type_id bigint NOT NULL REFERENCES equipment_types(id) ON DELETE CASCADE,
    present           integer NOT NULL DEFAULT 0 CHECK (present >= 0),
    working           integer NOT NULL DEFAULT 0 CHECK (working >= 0),
    PRIMARY KEY (room_number, equipment_type_id),
    CHECK (working <= present)
);

-- Seeded with the equipment the hotel's categories are actually made of. No
-- categories are seeded: the shapes ("Standard", "Junior", "Standard Premium")
-- differ per hotel and are the manager's to define.
INSERT INTO equipment_types (name, icon) VALUES
    ('TV', 'tv'),
    ('Phone', 'phone'),
    ('Router', 'server'),
    ('Access Point', 'wifi'),
    ('Door lock', 'lock')
ON CONFLICT (name) DO NOTHING;
