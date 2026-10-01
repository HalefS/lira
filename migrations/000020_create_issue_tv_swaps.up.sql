-- TV swaps: when a set in a room is broken and we have no spare, a working one
-- is taken from another room and the two are swapped.
--
-- This is a child table rather than two columns on `issues` because an issue can
-- move a TV more than once — out of the faulty room and into storage, then back
-- when the repair is done — and each of those is a distinct event worth keeping.
--
-- The rooms are free text rather than a reference: there is no rooms table in
-- this system (an issue's `location` is itself free text), and swaps are not
-- always between guest rooms — a set can come from a department or from storage.
CREATE TABLE IF NOT EXISTS issue_tv_swaps (
    id          bigserial PRIMARY KEY,
    created_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    issue_id    bigint NOT NULL REFERENCES issues ON DELETE CASCADE,

    -- Where the working TV came from, and where it went. Both are required: a
    -- swap with only one end recorded does not say which room got fixed.
    from_room   text NOT NULL CHECK (btrim(from_room) <> ''),
    to_room     text NOT NULL CHECK (btrim(to_room) <> ''),

    -- Free text for what a room pair cannot say: which set was moved, that the
    -- faulty one is awaiting repair, and so on.
    notes       text NOT NULL DEFAULT '',

    -- Nullable so removing a user never deletes the record of a swap they
    -- logged. Users are normally deactivated rather than deleted.
    created_by  bigint REFERENCES users ON DELETE SET NULL,
    version     integer NOT NULL DEFAULT 1,

    -- Swapping a TV with itself records nothing useful and is almost always a
    -- mistyped room number, so it is refused at the database as well as in Go.
    -- Compared case-insensitively so "1324" and " 1324 " cannot both be used
    -- to slip past the Go-side check.
    CONSTRAINT issue_tv_swaps_distinct_rooms CHECK (
        lower(btrim(from_room)) <> lower(btrim(to_room))
    )
);

CREATE INDEX IF NOT EXISTS issue_tv_swaps_issue_idx ON issue_tv_swaps (issue_id);
CREATE INDEX IF NOT EXISTS issue_tv_swaps_created_idx ON issue_tv_swaps (created_at DESC);

-- Which issue types offer to record a swap.
--
-- This is a property of the type rather than a hardcoded list of names so that
-- renaming a type cannot silently stop the prompt from appearing, and so the
-- same mechanism can be switched on for other equipment later (encoders,
-- remotes) without touching any code.
ALTER TABLE issue_types
    ADD COLUMN IF NOT EXISTS tracks_swaps boolean NOT NULL DEFAULT false;

-- Existing installs: switch it on for the type that is already called TV, so
-- the feature works from the moment it is deployed instead of waiting for a
-- manager to open the admin page.
UPDATE issue_types SET tracks_swaps = true WHERE lower(btrim(name)) = 'tv';
