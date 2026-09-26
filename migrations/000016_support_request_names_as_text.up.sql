-- The two Telnet fields become plain text instead of references to our own
-- `users` table. They are the names of people at Telnet / whoever we spoke to
-- on the phone, which our user list never contained — a dropdown could only
-- ever offer the wrong people, so the text is typed in by hand.
--
-- Losing the foreign keys is a real trade: the names are no longer checked
-- against anything, so they can misspell and can drift if someone is renamed.
-- In exchange the columns can no longer be nulled out from under us when a user
-- is deleted, which means the "a Telnet handover must name a technician" rule
-- can now be enforced by the database (see the CHECK below) instead of only by
-- the Go validator.
--
-- The Telefónica ticket id was already free text, so it is unaffected.
ALTER TABLE issue_support_requests
    ADD COLUMN IF NOT EXISTS telnet_technician   text,
    ADD COLUMN IF NOT EXISTS telnet_confirmed_by text;

-- Carry the existing names over before the ids they came from are dropped.
UPDATE issue_support_requests s
SET telnet_technician = u.name
FROM users u
WHERE u.id = s.telnet_technician_id AND s.telnet_technician IS NULL;

UPDATE issue_support_requests s
SET telnet_confirmed_by = u.name
FROM users u
WHERE u.id = s.telnet_confirmed_by_id AND s.telnet_confirmed_by IS NULL;

-- The old CHECK is written in terms of the id columns, so it goes before them.
ALTER TABLE issue_support_requests
    DROP CONSTRAINT IF EXISTS issue_support_requests_company_fields;

ALTER TABLE issue_support_requests
    DROP COLUMN IF EXISTS telnet_technician_id,
    DROP COLUMN IF EXISTS telnet_confirmed_by_id;

-- Coherence between the company and its fields, plus the two rules that were
-- previously left to Go: a Telnet handover always names who it was reported
-- to, and one marked solved also names who reported the fix. Unlike before,
-- no user deletion can turn a stored name into NULL, so these hold forever.
ALTER TABLE issue_support_requests ADD CONSTRAINT issue_support_requests_company_fields CHECK (
    (company = 'telnet'     AND telefonica_ticket_id IS NULL
                             AND telnet_technician IS NOT NULL
                             AND btrim(telnet_technician) <> ''
                             AND (status <> 'solved'
                                  OR (telnet_confirmed_by IS NOT NULL
                                      AND btrim(telnet_confirmed_by) <> '')))
    OR
    (company = 'telefonica' AND telefonica_ticket_id IS NOT NULL
                             AND telnet_technician IS NULL
                             AND telnet_confirmed_by IS NULL)
);
