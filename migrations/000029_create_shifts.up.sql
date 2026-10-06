-- Weekly team shift rota: the shift templates a manager configures on Settings,
-- and who is on which of them on which weekday.
--
-- Two tables, because they answer two different questions and are edited on two
-- different timescales. A shift is a definition -- "Night, 22:00 to 06:00" --
-- that belongs to nobody and is reused by whoever is on it. An assignment is one
-- cell of the grid: this person, this weekday, that shift. Collapsing them would
-- make every edit to a shift's hours an edit to every cell using it, and renaming
-- "Night" would be a data migration.
--
-- The rota is RECURRING. There is no date column anywhere in this feature, and
-- that is the whole design: the same seven assignments apply to every week until
-- somebody changes them. So an assignment is keyed by weekday, not by a calendar
-- date, and "what does Monday look like on the 3rd of November" is answered by the
-- same row as "on the 3rd of December". A table of per-date rows would be a
-- different feature -- holiday cover, absence, sick swaps -- and would need a way
-- to say "this week except that one".
--
-- Times are plain "HH:MM" text, exactly as issues.start_time and issues.end_time
-- are stored (migration 000007), for the reason given there: a straight
-- round-trip with <input type="time">, and no lib/pq TIME-column scanning that
-- would hand the JSON "08:00:00" and blank every cell in the grid. The two CHECKs
-- below are the database half of data.clockTimePattern and must agree with it
-- exactly.
--
-- end_time earlier than start_time is LEGAL and is what a night shift is: 22:00
-- to 06:00 finishes the following morning. There is deliberately no "end after
-- start" constraint, because adding one would make most of a 24-hour hotel's rota
-- un-storable. What is refused is the two being equal, which is a zero-length
-- shift that renders as a cell nobody is ever on. Whether a shift crosses
-- midnight is derived at read time rather than stored, so a flag can never
-- disagree with the two times it is about.
--
-- No seed rows. Unlike departments or issue types there is no set of shifts every
-- hotel shares -- "Morning" and "Night" mean whatever this hotel's rota means --
-- and inventing some would put shifts in the cell picker that nobody chose. An
-- empty shifts table is a real state that both the API and the page handle: the
-- grid renders its seven columns and every member's row of empty cells, and the
-- Settings card asks a manager to add a shift.
CREATE TABLE IF NOT EXISTS shifts (
    id          bigserial PRIMARY KEY,
    created_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),

    -- What the rota calls it: "Morning", "Night", "Late". citext so "Night" and
    -- "night" cannot both exist and leave two identical-looking options in the
    -- cell picker -- the same treatment issue_types, departments,
    -- consumable_items and connecta_agents all get.
    name        citext NOT NULL UNIQUE CHECK (btrim(name::text) <> ''),

    start_time  text NOT NULL,
    end_time    text NOT NULL,

    -- Kept but no longer offered when picking a cell. NOT NULL DEFAULT true so
    -- "the manager did not say" and "retired" cannot be confused.
    active      boolean NOT NULL DEFAULT true,

    created_by  bigint REFERENCES users ON DELETE SET NULL,
    updated_by  bigint REFERENCES users ON DELETE SET NULL,

    -- Optimistic locking for the Settings editor. A shift is a small record a
    -- manager edits from a form, so two of them can hold the same version and one
    -- of the saves has to lose -- the same reason rooms, users and
    -- maintenance_schedules all carry one.
    version     integer NOT NULL DEFAULT 1,

    CONSTRAINT shifts_start_time_check
        CHECK (start_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    CONSTRAINT shifts_end_time_check
        CHECK (end_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    -- The only ordering rule that exists. end < start is a night shift; end =
    -- start is nobody's shift at all.
    CONSTRAINT shifts_distinct_times
        CHECK (start_time <> end_time)
);

-- The Settings list and the grid's legend are read in the order a rota happens
-- in, not alphabetically: Morning, Afternoon, Night. A shift called "Zulu"
-- belongs at 22:00, not at the bottom of an A-Z list. id breaks ties so two
-- shifts starting together cannot swap places between requests. At a dozen rows
-- the sort is free either way; this exists so the order is the database's and the
-- two places that read it cannot drift.
CREATE INDEX IF NOT EXISTS shifts_active_start_idx
    ON shifts (active, start_time);

-- One row per person per weekday: which of the shifts they are on that day, or
-- no row at all for a day off.
--
-- The primary key IS the "one shift per (user, weekday)" rule. There is no separate
-- UNIQUE index because the constraint and the key are the same thing, and an id
-- column would be a second identity for a row that already has one. It also
-- happens to be the index every read of the grid wants: the whole assignment set
-- for a handful of members is a range scan on the left of the key.
--
-- Weekday is ISO 8601: 1 = Monday ... 7 = Sunday. Not Sunday-first and not
-- zero-based, because time.Time's Weekday() is Sunday-first with Sunday = 0 and
-- getting that conversion wrong is invisible -- it would just shift every column
-- one to the left. ISO is also what WeekRange already produces and what the grid's
-- columns are indexed by.
--
-- No version column here, unlike on shifts. An assignment is only ever written by
-- a PUT that carries the caller's whole row, so there is no read-modify-write
-- cycle for a second manager to interleave with; two managers setting the same
-- cell resolve to whoever wrote last, which is the right answer for a rota and is
-- what LCUModel.RecordTest and SettingsModel.Update already do.
--
-- ON DELETE CASCADE on both sides, for two different reasons.
--
--   * shift_id: an assignment is a cell in a live, repeating grid, not a
--     historical record of something that happened. There is no date on the row,
--     so cascading it destroys no fact about when anybody worked -- the date lives
--     in the week the reader is looking at, not here. Compare
--     maintenance_checks, which cascades because a daily log without its device is
--     meaningless, and maintenance_schedules.department_id, which is RESTRICT
--     because a service history "cannot be recovered from anywhere else". Neither
--     argument reaches a rota cell that points at a shift nobody has defined any
--     more: there is nothing in it to recover.
--
--     RESTRICT would also make a typo unrecoverable -- add "Night 22:00-06:00",
--     assign it to three people, add "Night" properly, then try to delete the
--     first -- unless the manager first went and cleared every cell referencing it,
--     one at a time, through a UI with no affordance for that. SET NULL would be
--     worse than either: a nullable shift_id invents a third grid state, "assigned
--     to a shift that no longer exists", that no read query can render and the page
--     has no cell style for.
--
--   * user_id: a person who is not in the team has no rota. Accounts are normally
--     deactivated rather than deleted, so this cascade rarely fires -- but when one
--     is genuinely removed its seven cells should go with it rather than sit there
--     pointing at a user id nothing joins to any more. Same shape as the reasoning
--     in migration 000026: what a leaver releases is the work, and here the correct
--     release is for the row to disappear.
CREATE TABLE IF NOT EXISTS shift_assignments (
    user_id     bigint NOT NULL REFERENCES users ON DELETE CASCADE,
    weekday     smallint NOT NULL CHECK (weekday BETWEEN 1 AND 7),
    shift_id    bigint NOT NULL REFERENCES shifts ON DELETE CASCADE,

    updated_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_by  bigint REFERENCES users ON DELETE SET NULL,

    PRIMARY KEY (user_id, weekday)
);

-- The FK column needs its own index: without one, deleting a shift -- a rare,
-- deliberate, manager-driven act -- would scan the whole assignment table to find
-- the rows to cascade away. The same index answers "how many cells use this
-- shift", which is what the delete confirmation reads so it can say what deleting
-- it would cost rather than quoting a count nobody kept.
CREATE INDEX IF NOT EXISTS shift_assignments_shift_idx
    ON shift_assignments (shift_id);