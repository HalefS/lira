ALTER TABLE issues
    DROP COLUMN IF EXISTS start_time,
    DROP COLUMN IF EXISTS end_time;
