-- Room inventory.
--
-- Apartment issues name a room, and until now that room was free text typed into
-- a field: "1214", "1214 ", "12l4" and "1214 " were four different rooms as far as
-- the database was concerned, and nothing could tell a technician that the room
-- they were about to type did not exist. This table is the answer to that, and it
-- is enforced on the way in rather than trusted.
--
-- One row is one run of consecutive rooms, not one room. A hotel with three
-- hundred rooms should not be three hundred rows to maintain, and the people
-- who know the numbering know it as "floor 1 is 1201 to 1212", not as three
-- hundred separate facts. So a single room is the degenerate case where
-- from_room = to_room, and there is no second concept to learn.
--
-- Numbers, not text. That is a deliberate narrowing: the numbering is what makes
-- a range meaningful, because it lets "does 1205 exist?" and "does this new range
-- overlap one I already have?" both be answered without expanding anything.
-- A hotel with a room called "Penthouse" cannot list it here; the constraint is
-- on the inventory, not on issues, so an apartment issue logged against an
-- unusual room before this table existed can still be read and edited.
--
-- Overlap is refused in Go rather than here, because a CHECK cannot express it.
-- Two rows covering 1201-1210 and 1205-1215 would make "is 1207 a room?" have two
-- answers, and a dropdown listing it twice is worse than no dropdown.
--
-- from_room <= to_room with a single range check, and the positivity check, so a
-- reversed or nonsensical run is refused at the database as well as in Go.
CREATE TABLE IF NOT EXISTS rooms (
    id          bigserial PRIMARY KEY,
    from_room   integer NOT NULL,
    to_room     integer NOT NULL,
    created_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    created_by  bigint REFERENCES users ON DELETE SET NULL,
    version     integer NOT NULL DEFAULT 1,
    CONSTRAINT rooms_ordered    CHECK (from_room <= to_room),
    CONSTRAINT rooms_positive   CHECK (from_room > 0),
    -- An exact duplicate is the commonest mistake and the one with an obvious
    -- fix, so it is caught by the database even if the overlap check in Go is
    -- ever bypassed. Overlaps that are not identical still need Go.
    CONSTRAINT rooms_unique_run UNIQUE (from_room, to_room)
);

CREATE INDEX IF NOT EXISTS rooms_from_idx ON rooms(from_room);
CREATE INDEX IF NOT EXISTS rooms_to_idx   ON rooms(to_room);

-- Seed from the rooms that already exist in the data.
--
-- Every distinct apartment-issue location that is a plain number becomes a
-- one-room run, so the day this lands nobody loses the ability to log an issue
-- for a room they could log an issue for yesterday. Locations that are not
-- numbers are deliberately skipped: the test data in this database includes
-- "TEST" and "CONTEST", and seeding those would put them in the dropdown as if
-- they were rooms. Those issues are not orphaned by this -- an apartment issue
-- keeps its stored room when editing even if the room is not in the inventory,
-- the same way a department issue keeps a department name that has since been
-- removed from the catalog.
--
-- The DISTINCT matters: 1324 and 4120 each appear on more than one issue.
INSERT INTO rooms (from_room, to_room)
SELECT DISTINCT location::integer, location::integer
FROM issues
WHERE mode = 'apt'
  AND location ~ '^[0-9]+$'
  AND location::integer > 0
ORDER BY 1;
