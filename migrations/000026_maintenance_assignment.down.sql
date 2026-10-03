DROP INDEX IF EXISTS maintenance_schedules_assignee_idx;

-- Drop the column rather than nulling it: this is the down path, and leaving a
-- column that does nothing would be worse than not having it.
ALTER TABLE maintenance_schedules
    DROP COLUMN IF EXISTS assignee_id;