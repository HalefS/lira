-- Return to the fixed-column shape from migration 000043. The configured values
-- are copied back into the columns that migration understood before they are
-- dropped, so handovers to Telnet, Telefónica and a Settings-added reference
-- company keep their data. Fields with no fixed column cannot be represented
-- here; restoring the old CHECK fails loudly rather than silently dropping
-- them, which is the honest outcome for a rollback past this point.
DROP INDEX IF EXISTS issue_support_requests_primary_reference_key;

UPDATE issue_support_requests s
SET telnet_technician   = CASE WHEN s.company = 'telnet' THEN NULLIF(s.fields->>'technician', '') ELSE NULL END,
    telnet_confirmed_by = CASE WHEN s.company = 'telnet' THEN NULLIF(s.fields->>'confirmed_by', '') ELSE NULL END,
    telefonica_ticket_id = CASE WHEN s.company = 'telefonica' THEN NULLIF(s.fields->>'ticket_id', '') ELSE NULL END,
    reference           = CASE WHEN s.company NOT IN ('telnet', 'telefonica')
                               THEN COALESCE(NULLIF(s.fields->>'reference', ''), s.reference_summary)
                               ELSE NULL END;

ALTER TABLE issue_support_requests
    DROP COLUMN IF EXISTS primary_reference,
    DROP COLUMN IF EXISTS reference_summary,
    DROP COLUMN IF EXISTS fields;

DROP TABLE IF EXISTS support_company_fields;

ALTER TABLE issue_support_requests
    ADD CONSTRAINT issue_support_requests_company_fields CHECK (
        (company = 'telnet'
            AND telefonica_ticket_id IS NULL
            AND reference IS NULL
            AND telnet_technician IS NOT NULL
            AND btrim(telnet_technician) <> ''
            AND (status <> 'solved'
                 OR (telnet_confirmed_by IS NOT NULL
                     AND btrim(telnet_confirmed_by) <> '')))
        OR
        (company = 'telefonica'
            AND telefonica_ticket_id IS NOT NULL
            AND reference IS NULL
            AND telnet_technician IS NULL
            AND telnet_confirmed_by IS NULL)
        OR
        (company NOT IN ('telnet', 'telefonica')
            AND reference IS NOT NULL
            AND btrim(reference) <> ''
            AND telefonica_ticket_id IS NULL
            AND telnet_technician IS NULL
            AND telnet_confirmed_by IS NULL)
    );
