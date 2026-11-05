-- More than one device per account.
--
-- The original rule was deliberate: signing in again revokes the previous token,
-- which is right for a shared reception desktop. It is wrong for a phone. A
-- technician signing in on the phone would silently sign the desk out, and the
-- desk would sign the phone out on its next shift.
--
-- A device_id is the stable identifier the browser or installed app keeps. Login
-- revokes only tokens for the same account and the same device_id; a login with
-- no device_id keeps the old "revoke everything" behaviour, so an older client
-- cannot accidentally leave a session behind.
--
-- Empty strings rather than NULLs: the table predates this distinction, and a
-- NOT NULL default keeps every query free of three-valued logic. The partial
-- index mirrors the login lookup.
ALTER TABLE tokens ADD COLUMN IF NOT EXISTS device_id text NOT NULL DEFAULT '';
ALTER TABLE tokens ADD COLUMN IF NOT EXISTS device_name text NOT NULL DEFAULT '';
ALTER TABLE tokens ADD COLUMN IF NOT EXISTS created_at timestamp with time zone NOT NULL DEFAULT NOW();

CREATE INDEX IF NOT EXISTS tokens_user_scope_device_idx
    ON tokens (user_id, scope, device_id)
    WHERE device_id <> '';
