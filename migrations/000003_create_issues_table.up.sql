-- migrations/000003_create_issues_table.up.sql
CREATE TABLE IF NOT EXISTS issues (
    id              bigserial PRIMARY KEY,
    created_at      timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    mode            text NOT NULL CHECK (mode IN ('apt', 'dept')),
    location        text NOT NULL,   -- apartment number OR department name
    type            text NOT NULL,
    problem         text NOT NULL,
    resolution      text NOT NULL DEFAULT '',
    time_minutes    integer NOT NULL DEFAULT 0,
    status          text NOT NULL DEFAULT 'Pending' CHECK (status IN ('Ok', 'Pending')),
    logged_by       bigint NOT NULL REFERENCES users ON DELETE SET NULL,
    version         integer NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS issues_mode_idx       ON issues(mode);
CREATE INDEX IF NOT EXISTS issues_status_idx     ON issues(status);
CREATE INDEX IF NOT EXISTS issues_logged_by_idx  ON issues(logged_by);
CREATE INDEX IF NOT EXISTS issues_created_at_idx ON issues(created_at);
