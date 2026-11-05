DROP INDEX IF EXISTS tokens_user_scope_device_idx;
ALTER TABLE tokens DROP COLUMN IF EXISTS created_at;
ALTER TABLE tokens DROP COLUMN IF EXISTS device_name;
ALTER TABLE tokens DROP COLUMN IF EXISTS device_id;
