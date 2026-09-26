-- Puts the Telnet fields back as references to `users`. The names are matched
-- back to user rows by name; anything that doesn't match a current user is lost,
-- because that is exactly the information the old schema did not store.
ALTER TABLE issue_support_requests
    ADD COLUMN IF NOT EXISTS telnet_technician_id   bigint REFERENCES users ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS telnet_confirmed_by_id bigint REFERENCES users ON DELETE SET NULL;

UPDATE issue_support_requests s
SET telnet_technician_id = u.id
FROM users u
WHERE u.name = s.telnet_technician AND s.telnet_technician_id IS NULL;

UPDATE issue_support_requests s
SET telnet_confirmed_by_id = u.id
FROM users u
WHERE u.name = s.telnet_confirmed_by AND s.telnet_confirmed_by_id IS NULL;

ALTER TABLE issue_support_requests
    DROP CONSTRAINT IF EXISTS issue_support_requests_company_fields;

ALTER TABLE issue_support_requests
    DROP COLUMN IF EXISTS telnet_technician,
    DROP COLUMN IF EXISTS telnet_confirmed_by;

-- The original rule set: the technician requirement is left to Go again, so
-- that removing the user later can never fail this check.
ALTER TABLE issue_support_requests ADD CONSTRAINT issue_support_requests_company_fields CHECK (
    (company = 'telnet'     AND telefonica_ticket_id IS NULL) OR
    (company = 'telefonica' AND telefonica_ticket_id IS NOT NULL
                                   AND telnet_technician_id IS NULL
                                   AND telnet_confirmed_by_id IS NULL)
);
