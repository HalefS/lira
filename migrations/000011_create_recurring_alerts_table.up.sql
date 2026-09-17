-- Tracks the resolution state of a recurring-issue alert (a mode/location/
-- type combination that's happened more than once). Alerts are detected
-- live from the issues table, but their pending/solved status needs to
-- persist independently of that detection window, so it lives here.
CREATE TABLE IF NOT EXISTS recurring_alerts (
    id         bigserial PRIMARY KEY,
    mode       text NOT NULL,
    location   text NOT NULL,
    type       text NOT NULL,
    status     text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'solved')),
    solution   text,
    solved_by  bigint REFERENCES users ON DELETE SET NULL,
    solved_at  timestamp(0) with time zone,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_at timestamp(0) with time zone NOT NULL DEFAULT NOW()
);

-- One tracked alert per (mode, location, type), matched the same
-- case/whitespace-insensitive way the recurring-issue detection itself
-- compares locations.
CREATE UNIQUE INDEX IF NOT EXISTS recurring_alerts_key_idx
    ON recurring_alerts (mode, LOWER(TRIM(location)), type);
