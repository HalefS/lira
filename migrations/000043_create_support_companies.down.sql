-- Rolling back loses every company added in Settings. Handovers to those
-- companies cannot satisfy the restored two-company CHECK, so this migration
-- fails loudly if any exist rather than silently deleting history.
DROP INDEX IF EXISTS issue_support_requests_reference_key;

ALTER TABLE issue_support_requests
    DROP CONSTRAINT IF EXISTS issue_support_requests_company_fields;

ALTER TABLE issue_support_requests
    DROP CONSTRAINT IF EXISTS issue_support_requests_company_fkey;

ALTER TABLE issue_support_requests
    DROP COLUMN IF EXISTS reference;

ALTER TABLE issue_support_requests
    ADD CONSTRAINT issue_support_requests_company_check
        CHECK (company IN ('telnet', 'telefonica'));

ALTER TABLE issue_support_requests
    ADD CONSTRAINT issue_support_requests_company_fields CHECK (
        (company = 'telnet'
            AND telefonica_ticket_id IS NULL
            AND telnet_technician IS NOT NULL
            AND btrim(telnet_technician) <> ''
            AND (status <> 'solved'
                 OR (telnet_confirmed_by IS NOT NULL
                     AND btrim(telnet_confirmed_by) <> '')))
        OR
        (company = 'telefonica'
            AND telefonica_ticket_id IS NOT NULL
            AND telnet_technician IS NULL
            AND telnet_confirmed_by IS NULL)
    );

DROP TABLE IF EXISTS support_companies;
