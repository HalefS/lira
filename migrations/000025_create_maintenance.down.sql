-- Order matters: the checks table references both the schedules and the issues
-- table, and dropping it first is what lets the schedules table go without a
-- dependency still pointing at it.
DROP INDEX IF EXISTS maintenance_checks_schedule_idx;
DROP TABLE IF EXISTS maintenance_checks;

DROP INDEX IF EXISTS maintenance_schedules_due_idx;
DROP TABLE IF EXISTS maintenance_schedules;