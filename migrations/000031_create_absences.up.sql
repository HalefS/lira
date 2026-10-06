-- Per-date absences: the dates a person is NOT on the rota, and why.
--
-- This is the feature migration 000029 explicitly ruled out of the rota. Those
-- assignments carry no date by design -- "what does Monday look like on the 3rd"
-- is answered by the same row as "on the 3rd of December" -- so "Ana is on holiday
-- the week of the 3rd" cannot possibly be a row in shift_assignments. A separate
-- table is the point. The rota stays the recurring pattern, absences stay the
-- dated exceptions to it, and neither grows a column for the other.
--
-- That separation is load-bearing rather than tidy. Putting a date on
-- shift_assignments would make the assignment table answer two different questions
-- with two different row shapes, and the grid read that returns all of it would
-- need a filter it currently must not have -- the exact "filter by week" mistake
-- schedule.go warns about at length, which fails silently by returning an empty
-- grid.
--
-- One row per SPAN, not one per day. A fortnight of annual leave is one fact and
-- one fact is one row; per-day makes the commonest entry a fourteen-row write, and
-- makes "extend it by two days" an insert that says nothing about what it extends.
-- starts_on is day one and ends_on is the LAST day, both inclusive -- a single day
-- is the same date twice, which is what a manager typing two dates means. Note
-- this is the opposite of WeekRange's half-open [start, start+7) form, and that is
-- fine: WeekRange's exclusivity is a query bound and never leaves the database.
--
-- Plain dates rather than timestamps, for the reason migration 000019 gives: this
-- is a run of calendar days in the hotel's local time, so a `date` is immune both
-- to DST and to the database session sitting in a different timezone from the
-- application.
--
-- Overlapping absences are ALLOWED, deliberately. Sick leave starting in the
-- middle of somebody's holiday is a real thing that gets recorded, and refusing it
-- would mean one of two true facts cannot be written. Because overlap is legal
-- there is no invariant to enforce, so there is no check-then-insert to make
-- race-safe -- the classic "SELECT ... FOR UPDATE locks nothing when it matched
-- nothing" trap has nothing to guard. The cost is that the grid must be able to
-- render two absences on one date, which is why ScheduleMember.Absences is a slice
-- per day rather than a pointer.
--
-- kind is the four real cases and nothing else, as a CHECK rather than a table,
-- for the reason maintenance_schedules.equipment_type is one: the set is fixed by
-- the application and four values long. reason sits beside it as unconstrained
-- free text because "Marco's wedding" is a real thing a manager needs to record
-- and it is not one the application knows about.
--
-- There is no UNIQUE and no EXCLUDE here, on purpose. A unique index could only
-- refuse two byte-identical spans, which is not a failure anybody hits; the real
-- failure -- two DIFFERENT absences overlapping -- is not expressible that way at
-- all. The textbook constraint for it is
--
--     EXCLUDE USING gist (user_id WITH =, daterange(starts_on, ends_on, '[]') WITH &&)
--
-- which needs the btree_gist extension, which this schema has never had, and
-- which would forbid the sick-leave-inside-holiday case above. If absence overlap
-- is ever made an error, that constraint plus a 23P01 handler is the whole change,
-- and it is one migration.
CREATE TABLE IF NOT EXISTS absences (
    id          bigserial PRIMARY KEY,
    created_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_at  timestamp(0) with time zone NOT NULL DEFAULT NOW(),

    -- ON DELETE CASCADE, for the reason 000029 gives for shift_assignments.user_id
    -- and in a stronger form. An absence is three facts -- a person, a kind, a run
    -- of dates -- and once the person is gone none of the three is answerable in
    -- the way this API answers anything else; the grid has no row to hang it on. A
    -- departed person has no leave record, exactly as they have no rota cells.
    --
    -- RESTRICT is the choice maintenance_schedules.department_id makes, for a
    -- service history that "cannot be recovered from anywhere else". That argument
    -- does not reach here: restricting would mean a manager could never remove a
    -- leaver until they had first deleted a vacation, through a UI with no
    -- affordance for that -- the unrecoverable-typo trap 000029 calls out for
    -- shifts. SET NULL is worse than either, because it invents an absence
    -- belonging to nobody, which no grid cell can render.
    --
    -- In practice this rarely fires: accounts are deactivated rather than deleted
    -- (deactivateUserHandler only flips active=false and revokes tokens), and a
    -- deactivated member keeps their absences, their rota row and their ordering,
    -- exactly as they keep their week.
    user_id     bigint NOT NULL REFERENCES users ON DELETE CASCADE,

    kind        text NOT NULL,

    starts_on   date NOT NULL,
    ends_on     date NOT NULL,

    -- Free text, and deliberately never served to an anonymous caller. The grid is
    -- a public noticeboard; "Ana is off" is what a noticeboard is for, and "Ana's
    -- note" is not. AbsenceSummary -- the shape that travels in GET /v1/schedule --
    -- has no reason field at all, so this cannot leak without a type change, which
    -- is a stronger guarantee than a conditional in a handler.
    reason      text NOT NULL DEFAULT '',

    created_by  bigint REFERENCES users ON DELETE SET NULL,
    updated_by  bigint REFERENCES users ON DELETE SET NULL,

    -- Optimistic locking, unlike shift_assignments. An absence span is EXTENDED by
    -- an UPDATE, so it genuinely is a read-modify-write and two managers can hold
    -- the same version. That is the whole difference from the rota cells, whose
    -- only writer is a PUT carrying the caller's whole row and which therefore has
    -- nothing to interleave.
    version     integer NOT NULL DEFAULT 1,

    CONSTRAINT absences_kind_check
        CHECK (kind IN ('vacation', 'sick', 'training', 'unavailable')),

    -- The only ordering rule, and the same one lcu_units has. ends_on < starts_on
    -- is not a zero-length absence, it is a typo: the range is inclusive at both
    -- ends, so a single day is legitimately the same date twice.
    CONSTRAINT absences_span_check
        CHECK (ends_on >= starts_on)
);

-- "Which absences touch these seven dates", for every member at once -- the one
-- read the grid needs. starts_on leads because it is the half of the predicate
-- that turns seven dates into one range rather than seven ORs, and because
-- starts_on <= week_end AND ends_on >= week_start can use it as the scan bound.
--
-- That predicate is two halves and BOTH are load-bearing. starts_on <= $2 alone
-- matches a six-month holiday for every date in the future and fails silently:
-- the grid would show somebody absent indefinitely. It is written exactly twice in
-- this codebase, in the two reads below, and nowhere else.
CREATE INDEX IF NOT EXISTS absences_window_idx
    ON absences (starts_on, ends_on);

-- The index the user_id FK needs: the primary key is on id, so without this a user
-- deletion scans the whole table to find the rows to cascade away -- the same
-- argument 000029 makes for shift_assignments_shift_idx. Doubles as "every absence
-- this one person has", newest window first, which is the per-member list.
CREATE INDEX IF NOT EXISTS absences_user_idx
    ON absences (user_id, starts_on DESC, id DESC);

COMMENT ON COLUMN absences.starts_on IS 'First day of the absence, inclusive.';
COMMENT ON COLUMN absences.ends_on   IS 'Last day of the absence, inclusive. A one-day absence is the same date twice.';
COMMENT ON COLUMN absences.kind IS
    'One of vacation, sick, training, unavailable. Free text belongs in reason, which is not constrained and is not served to anonymous callers.';