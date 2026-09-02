-- Single-row table of application-wide settings a manager can tune.
-- The `id = 1` check enforces there's ever only one row (a singleton).
CREATE TABLE IF NOT EXISTS app_settings (
    id                     smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    duplicate_window_hours integer NOT NULL DEFAULT 24 CHECK (duplicate_window_hours > 0),
    updated_at             timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_by             bigint REFERENCES users ON DELETE SET NULL
);

INSERT INTO app_settings (id, duplicate_window_hours) VALUES (1, 24)
ON CONFLICT (id) DO NOTHING;

-- Speeds up the recurring-issue lookup that runs every time a technician
-- picks a location + issue type while logging a new issue.
CREATE INDEX IF NOT EXISTS issues_mode_location_type_idx ON issues (mode, location, type);
