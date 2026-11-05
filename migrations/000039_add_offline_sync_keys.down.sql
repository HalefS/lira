DROP INDEX IF EXISTS issue_attachments_issue_client_uuid_key;
ALTER TABLE issue_attachments DROP COLUMN IF EXISTS client_uuid;

DROP INDEX IF EXISTS issues_logged_by_client_uuid_key;
ALTER TABLE issues DROP COLUMN IF EXISTS client_uuid;
