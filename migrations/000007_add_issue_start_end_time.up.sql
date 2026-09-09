-- Stored as plain "HH:MM" text (matching the <input type="time"> value
-- format exactly) rather than a native TIME column, to avoid driver-level
-- scanning quirks and keep it a straight round-trip with the frontend.
-- Nullable because issues logged before this migration have no recorded
-- start/end pair.
ALTER TABLE issues
    ADD COLUMN IF NOT EXISTS start_time text,
    ADD COLUMN IF NOT EXISTS end_time   text;
