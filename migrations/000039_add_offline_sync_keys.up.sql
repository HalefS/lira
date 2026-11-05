-- Offline clients retry. A lost response must not become a second issue.
--
-- The phone cannot know whether an issue POST succeeded when the connection
-- drops after the bytes left. It retries, and without a key the server has no
-- way to tell "this is the same issue again" apart from "a technician really did
-- log the same fault twice". The client therefore generates a UUID when the
-- issue is queued and sends it on every attempt.
--
-- The key is scoped to the account, not global: a UUID collision between two
-- users would be a curiosity, but scoping it means one account's key can never
-- block another's insert. Rows that predate this migration have NULL, which the
-- partial index leaves alone.
ALTER TABLE issues ADD COLUMN IF NOT EXISTS client_uuid uuid;

CREATE UNIQUE INDEX IF NOT EXISTS issues_logged_by_client_uuid_key
    ON issues (logged_by, client_uuid)
    WHERE client_uuid IS NOT NULL;

-- Photos take the same treatment, one level down.
--
-- A photo is uploaded after the issue exists and can be retried independently of
-- it. The unique key is per issue rather than per user: the same phone photo
-- retried against the same issue is a duplicate, while the same bytes attached
-- to two genuinely different issues are not ours to collapse.
ALTER TABLE issue_attachments ADD COLUMN IF NOT EXISTS client_uuid uuid;

CREATE UNIQUE INDEX IF NOT EXISTS issue_attachments_issue_client_uuid_key
    ON issue_attachments (issue_id, client_uuid)
    WHERE client_uuid IS NOT NULL;
