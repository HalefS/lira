ALTER TABLE issue_support_requests
    DROP CONSTRAINT IF EXISTS issue_support_requests_times;

DROP INDEX IF EXISTS issue_support_requests_created_at_idx;
DROP INDEX IF EXISTS issue_support_requests_duration_idx;

ALTER TABLE issue_support_requests
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS resolved_at,
    DROP COLUMN IF EXISTS duration_minutes;
