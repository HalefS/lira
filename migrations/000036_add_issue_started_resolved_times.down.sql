-- Drops the started_at / resolved_at columns.
--
-- WHAT THIS LOSES
--
-- Every date a manager or technician set on an issue's work window. After this,
-- start_time and end_time are the only record again, and they carry no date, so
-- an issue that was logged on Tuesday about work done on Monday reads as Monday's
-- issue with a Tuesday timestamp. The duration stored in time_minutes survives,
-- because it is a plain integer and is not touched here.
--
-- It also loses the only route BACK to dated work: the up migration backfills
-- started_at from start_time on the logging day, which is correct only the first
-- time. An issue that was deliberately backdated cannot be reconstructed from
-- what remains.
--
-- So: safe for a bad deploy, unsafe for a bad month. Note the weeks that had
-- backdated issues before running it.
--
-- Drop order: constraint, index, then the columns. The constraint references
-- both columns, so it must go first; the index is an expression index on
-- COALESCE(started_at, created_at) and must be dropped before started_at is.
ALTER TABLE issues DROP CONSTRAINT IF EXISTS issues_started_resolved_check;
DROP INDEX IF EXISTS issues_work_at_idx;
ALTER TABLE issues
    DROP COLUMN IF EXISTS resolved_at,
    DROP COLUMN IF EXISTS started_at;
-- start_time and end_time are untouched, so the table returns to exactly the
-- shape 000007 left it in.
