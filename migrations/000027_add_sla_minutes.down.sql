-- Reverses 000027_add_sla_minutes.
--
-- Dropping the column rather than nulling it: nothing else references the SLA, so
-- there is no data to preserve and no other object to drop first.

ALTER TABLE app_settings DROP CONSTRAINT app_settings_sla_minutes_check;
ALTER TABLE app_settings DROP COLUMN sla_minutes;
