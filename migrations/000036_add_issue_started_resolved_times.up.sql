-- Add the wall-clock moment work started and finished, so an issue can be
-- backdated or span more than one day.
--
-- WHY TWO COLUMNS AND NOT A DATE ON THE EXISTING ONES
--
-- start_time and end_time are text "HH:MM" with no date on them, which is the
-- original design's whole point: a rota issue happened on the day it was logged,
-- and the only thing that mattered was how long it took. That assumption is what
-- this migration exists to relax, and it is relaxed by ADDING rather than by
-- changing: the existing columns stay, their meaning stays, and every row that
-- ever had a clock time keeps it.
--
-- The new columns are timestamps, following the precedent set by support_requests
-- (000015, started_at / resolved_at). A date plus a time in one value cannot
-- drift apart the way two columns can, and it is what the report's half-open
-- interval filters need.
--
-- WHY NULLABLE
--
-- A Pending issue has no end, and a logged issue may have no start recorded at
-- all. Both are legitimate states, not missing data, and the UI shows them
-- differently. NOT NULL would force a placeholder timestamp and lose that
-- distinction.
--
-- EXPAND AND CONTRACT
--
-- This is the expand half. The old text columns are deprecated from this point
-- but are NOT dropped: old clients still send them, old rows still carry them,
-- and dropping now would make the migration irreversible in practice. A later
-- migration drops them once nothing reads them. Its .down.sql must state that
-- the HH:MM-only history is not recoverable, because by then the timestamps
-- will be the only record.
--
-- BACKFILL
--
-- Every existing row gets a non-null started_at, so reports and the UI never
-- have to handle "no work moment" -- it is always "the day the issue was
-- logged, at the recorded clock time, or at midnight if there was none". An
-- overnight issue (end time earlier than start time) puts the end on the
-- following day, matching the old ComputeDurationMinutes midnight-wrap rule so
-- that durations do not change on upgrade.
--
-- The resolved_at backfill is best-effort: a row whose end_time is missing
-- stays null, because "no end recorded" is a real state (a Pending issue) and
-- inventing one would be worse than leaving it.
ALTER TABLE issues
    ADD COLUMN IF NOT EXISTS started_at  timestamp(0) with time zone,
    ADD COLUMN IF NOT EXISTS resolved_at timestamp(0) with time zone;

-- started_at: the clock time on the logging day, or created_at itself.
UPDATE issues
SET started_at = created_at::date + start_time::time
WHERE started_at IS NULL
  AND start_time IS NOT NULL
  AND btrim(start_time) <> '';

UPDATE issues
SET started_at = created_at
WHERE started_at IS NULL;

-- resolved_at: the clock time on the logging day, crossing midnight when the
-- end time is earlier than the start time (overnight work). Only when there
-- was an end time at all.
UPDATE issues
SET resolved_at = CASE
    WHEN start_time IS NOT NULL AND btrim(start_time) <> ''
         AND end_time IS NOT NULL AND btrim(end_time) <> ''
         AND end_time::time <= start_time::time
        THEN created_at::date + INTERVAL '1 day' + end_time::time
    WHEN end_time IS NOT NULL AND btrim(end_time) <> ''
        THEN created_at::date + end_time::time
    ELSE NULL
END
WHERE resolved_at IS NULL
  AND end_time IS NOT NULL
  AND btrim(end_time) <> '';

-- Authoritative duration, recomputed from the two moments. time_minutes was
-- previously stored from a client-supplied value; from now on it is derived,
-- and every row is corrected on upgrade.
UPDATE issues
SET time_minutes = ROUND(EXTRACT(EPOCH FROM (resolved_at - started_at)) / 60.0)::int
WHERE started_at IS NOT NULL
  AND resolved_at IS NOT NULL
  AND resolved_at >= started_at;

-- A resolved moment can never precede the start, and a duration is never
-- negative. The constraint is CHECK rather than a trigger because both columns
-- are written by the same statement and the rule is about the row as a whole.
ALTER TABLE issues
    ADD CONSTRAINT issues_started_resolved_check CHECK (
        (resolved_at IS NULL OR (started_at IS NOT NULL AND resolved_at >= started_at))
        AND time_minutes >= 0
    );

-- Reports filter on the work moment, not the logging moment, via COALESCE so
-- that a row with no recorded start still lands on its logging day. An
-- expression index rather than a plain one: the query cannot use a plain
-- started_at index for those rows, and a plain created_at index would miss the
-- ones that were backdated.
CREATE INDEX IF NOT EXISTS issues_work_at_idx
    ON issues ((COALESCE(started_at, created_at)));
