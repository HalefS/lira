-- Per-week rota cells, so that editing one week stops rewriting every other week.
--
-- THE PROBLEM THIS FIXES
--
-- shift_assignments is a RECURRING PATTERN: it is keyed on (user_id, weekday) with no
-- date, so the same seven cells are painted onto whatever week is asked for. Editing
-- Tuesday therefore rewrote last Tuesday and next Tuesday, and a manager tidying this
-- week silently rewrote the record of what the team worked last week. That is the
-- single complaint that produced this migration.
--
--
-- WHAT IS IN HERE, AND WHAT IS NOT
--
-- shift_assignments STAYS, and keeps its meaning: it is the STANDING PATTERN -- the
-- default for any week that has no schedule of its own. It is not the current week, and
-- conflating the two is the mistake this table exists to allow people to stop making:
-- the current week is half elapsed and its elapsed days are a record, so it needs cells
-- of its own exactly as a future week does.
--
-- So there are three things, not two:
--
--   no rows for a week        the week INHERITS the standing pattern. This is what a
--                              future week looks like before anybody touches it, and it
--                              is why peeking ahead shows today's rota.
--   rows for a week           that week has a schedule of its own. Editing it touches
--                              nothing else.
--   rows and frozen_at set    that week is OVER and is now a record. It cannot be
--                              edited, and the trigger below is what makes that true
--                              against a direct psql session as well as through the app.
--
--
-- WHY shift_id IS NULLABLE, WHICH CONTRADICTS 000029
--
-- 000029 argues at length that a nullable shift_id is wrong here, and it is right for
-- shift_assignments: there, NULL would be the artefact of a deleted shift cascading in,
-- and a grid cell with no meaning is worse than no row at all.
--
-- Here NULL is a DECISION, not an accident of deletion, and that is the whole
-- distinction. The three row states are:
--
--   (no row)                 inherit the standing pattern
--   (row, shift_id set)      this shift, for this week
--   (row, shift_id NULL)     explicitly OFF, for this week only
--
-- Without the third state there is no way to say "Marco is off next Tuesday but works
-- every other Tuesday", because a day off in the pattern is expressed by the ABSENCE
-- of a row and cannot be made specific to one week.
--
-- The nullable FK is therefore safe ONLY because of the ON DELETE RESTRICT below. A
-- cascade would erase frozen history -- a cell for 2026-10-06 is a record of what
-- somebody worked, and cascading would destroy it when a manager tidied up a shift
-- template. 000029's argument that cascading "destroys no fact" is exactly INVERTED
-- here, because these rows have dates on them.
--
--
-- frozen_at IS A COLUMN AND NOT A CLOCK READ
--
-- "Is this week closed?" could be answered by comparing week_start against today(). It is
-- not, and the reason is transactional: a clock read has no transaction, so two managers
-- at 00:00:00 on Monday could disagree about whether the week is open. With frozen_at,
-- closing a week is a WRITE, and writes are ordered.
--
-- It is set by the app when a week ends -- in the same transaction as any edit that would
-- otherwise invalidate it, and lazily for a week nobody looked at. The honest limit of
-- that: a week is exact if it was frozen before the first standing-pattern edit that
-- followed its close. A rota nobody edited for two weeks was a rota nobody changed, so
-- inheriting is then correct rather than approximate -- the failure needs BOTH an
-- unvisited week AND an edit to the pattern, and the edit is itself a rota write, which
-- freezes the weeks that preceded it.
--
--
-- WHY THERE IS NO BACKFILL, AND THE FIELD THAT ADMITS IT
--
-- Every week before this migration genuinely WAS the standing pattern, because there was
-- only ever one pattern. So an empty past week rendering the pattern looks right.
--
-- It stops being right the moment the standing pattern is first changed, and at that
-- moment a backfilled past week becomes a fabrication wearing a timestamp -- worse than
-- an empty cell, because it looks like a record. So there is no backfill, and
-- ScheduleWeek.HistoryExact is false for any past week with no cells, which the UI says
-- out loud. That is the difference between a rota that lies and one that admits it does
-- not remember.
--
-- ONE TABLE, NOT TWO
--
-- A separate snapshot table would make every read union two sources and decide which
-- wins. Here the PK is the same for a frozen record and a future override, so the read is
-- one query with a WHERE and the semantics live in one place. Age is derivable from
-- week_start and does not belong in a column that can then disagree with the clock.
CREATE TABLE IF NOT EXISTS shift_week_cells (
    -- The Monday of the week, as a DATE. Not a timestamp, for the reason 000031 gives:
    -- a week boundary is a calendar day, immune to DST and to the database session
    -- sitting in a different timezone from the application.
    week_start  date    NOT NULL,

    user_id     bigint  NOT NULL REFERENCES users ON DELETE CASCADE,
    weekday     smallint NOT NULL CHECK (weekday BETWEEN 1 AND 7),

    -- See the header: NULL here means "explicitly off this week", which is only safe
    -- because the reference RESTRICTS rather than cascades.
    shift_id    bigint  REFERENCES shifts ON DELETE RESTRICT,

    -- Set when the week closes. NULL means still open. Never cleared.
    frozen_at   timestamp(0) with time zone,

    created_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_by  bigint REFERENCES users ON DELETE SET NULL,

    PRIMARY KEY (week_start, user_id, weekday)
);

-- The read is "one week, every member"; the PK already leads with week_start, so this
-- serves the other direction -- every week's cells for one member, which is what
-- materialising a future week from the pattern does.
CREATE INDEX IF NOT EXISTS shift_week_cells_user_idx
    ON shift_week_cells (user_id, week_start);

-- "Is this shift in any closed week?" -- the guard that stops a shift being deleted out
-- from under the history. Partial, because a cell with no shift is not a reference.
CREATE INDEX IF NOT EXISTS shift_week_cells_shift_idx
    ON shift_week_cells (shift_id)
    WHERE shift_id IS NOT NULL;

-- Closed weeks, in the order the sweeper closes them.
CREATE INDEX IF NOT EXISTS shift_week_cells_frozen_idx
    ON shift_week_cells (week_start) WHERE frozen_at IS NOT NULL;


-- A frozen cell is a record of what was scheduled and worked. Application code already
-- refuses to write one; this makes the refusal true against a direct psql session too,
-- which is the only way a rule about records is actually a rule rather than a convention.
--
-- A trigger rather than a CHECK, because a CHECK is evaluated on the row being INSERTed
-- or UPDATEd and the column is set BY the freezing statement -- so `frozen_at IS NULL`
-- would forbid every insert, and `frozen_at IS NOT NULL` would forbid every freeze.
-- There is no declarative form of "this column, once set, may not change".
--
-- TG_OP IS CHECKED, and that is not tidiness. In a BEFORE DELETE trigger NEW is NULL,
-- so a body ending in `RETURN NEW` asks Postgres to cancel the operation -- and
-- Postgres honours it. The guard would then have blocked EVERY delete of an unfrozen
-- cell while looking, from the outside, like a guard that only touches frozen ones: no
-- error, no warning, just rows that quietly refused to go away, and an
-- "empty this person's week" write that collided with the very rows it meant to
-- replace. Return OLD for a delete and NEW otherwise, which is the only thing that
 distinguishes the two operations.
CREATE OR REPLACE FUNCTION shift_week_cells_freeze_guard() RETURNS trigger AS $$
BEGIN
    IF OLD.frozen_at IS NOT NULL THEN
        RAISE EXCEPTION 'week % is closed and cannot be changed', OLD.week_start
            USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER shift_week_cells_no_edit_frozen
    BEFORE UPDATE OR DELETE ON shift_week_cells
    FOR EACH ROW EXECUTE FUNCTION shift_week_cells_freeze_guard();
