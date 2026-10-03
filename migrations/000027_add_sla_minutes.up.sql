-- Service level agreement: the resolution time an issue is allowed to take before
-- it is shown as breached.
--
-- One threshold for the whole application, not one per issue type. That is a
-- deliberate choice rather than a simplification: a per-type table would leave a
-- newly added issue type with no target at all until someone remembered to set
-- one, and a type with no target silently looks compliant.
--
-- NULL means no SLA is configured, and nothing is ever flagged. That is different
-- from zero: a zero threshold would mark every issue that took any time at all as
-- breached, including a three-minute fix. An unset SLA has to mean "no opinion",
-- not "everything fails".

ALTER TABLE app_settings
    ADD COLUMN sla_minutes integer;

ALTER TABLE app_settings
    ADD CONSTRAINT app_settings_sla_minutes_check
    CHECK (sla_minutes IS NULL OR sla_minutes > 0);

COMMENT ON COLUMN app_settings.sla_minutes IS
    'Resolution time in minutes allowed before an issue is shown as breaching the SLA. NULL disables the check.';
