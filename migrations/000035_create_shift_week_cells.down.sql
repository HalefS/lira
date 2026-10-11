-- Drops the per-week rota cells and their immutability trigger.
--
-- Trigger, then function, then table, for the reason 000031's down migration gives: each
-- is defined in terms of the one after it.
--
-- What this loses, and what it does not:
--
--   shift_assignments is UNTOUCHED. The standing pattern is exactly as it was, so after
--   this every week inherits again and every week is editable again -- which is the
--   behaviour this whole feature was built to replace. That is the sharp end of it, and
--   it is worth saying plainly: a rollback does not merely lose history, it restores the
--   bug that editing one week rewrites all of them.
--
-- What IS lost is every per-week edit made since deploy: any week that diverged from
-- the pattern, and any week that was closed and recorded. Unlike a rota_members
-- rollback this is NOT recoverable by re-running the up migration, because the app does
-- not store the standing pattern's history -- a week that was frozen recorded what the
-- pattern said at that moment, and that moment is gone. Re-running the migration creates
-- an empty table, and every past week reads as "inherits the pattern".
--
-- So the honest line: this down migration is safe for a bad deploy and unsafe for a bad
-- fortnight. Before running it, note which weeks have cells:
--
--     SELECT week_start, count(*) FROM shift_week_cells GROUP BY 1 ORDER BY 1;
--
--
-- DROP ORDER, and it is forced
--
-- Trigger, then function, then table, and each is defined in terms of the one after it.
--
-- IF EXISTS is used on every statement, and that is not defensive habit. This down
-- migration is the FIRST thing that removes these objects, so on any database where it
-- runs there is by definition something to drop -- but it is also the thing a person
-- reaches for after having applied the up migration by hand with psql, which is the
-- single most likely way to get here. IF EXISTS makes both of those work, and a down
-- migration that fails halfway leaves the schema in a state no migration can describe.
--
-- The trigger is named explicitly and dropped before its function: dropping the
-- function alone would leave a trigger pointing at nothing.
DROP TRIGGER IF EXISTS shift_week_cells_no_edit_frozen ON shift_week_cells;

-- The indexes go with the table, so they are not dropped separately. 000034's down
-- migration has to name them because it drops a table that other tables' indexes
-- outlive; these three belong to shift_week_cells alone.
DROP FUNCTION IF EXISTS shift_week_cells_freeze_guard();

DROP TABLE IF EXISTS shift_week_cells;
