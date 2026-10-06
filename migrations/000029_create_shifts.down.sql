-- Drops the rota.
--
-- Order matters: shift_assignments references shifts, so it goes first, for the
-- same reason 000025_create_maintenance.down.sql drops maintenance_checks before
-- maintenance_schedules.
--
-- There is nothing outside these two tables to preserve, and the honest cost of
-- that is worth stating plainly rather than discovering later: this migration
-- seeds nothing, so re-running the up migration recreates an empty rota and
-- nothing that was in it is recoverable from anywhere else. That is acceptable
-- because the rota is a configuration surface rather than a record of events --
-- the recurring pattern, seven columns wide, not a log of who was on nights in
-- March. It is also the same reason rooms' down migration can say its seed "goes
-- with it" and be re-derived from issues.location: here there is nothing to
-- re-derive from, and nothing claims otherwise.
DROP INDEX IF EXISTS shift_assignments_shift_idx;
DROP TABLE IF EXISTS shift_assignments;

DROP INDEX IF EXISTS shifts_active_start_idx;
DROP TABLE IF EXISTS shifts;