-- Issues we handed over to one of the external companies that support the
-- building: Telnet and Telefonica. An issue can need more than one handover,
-- and can be handed to both companies, so this is a child table with one row
-- per handover rather than a pair of columns on `issues` — that keeps the
-- history of who was chased and when.
--
-- Each row also carries its own status, independent of the issue's own
-- Ok/Pending status. An issue is usually left Pending for a long time while we
-- wait on a third party, and the handover itself has to be trackable ("still
-- waiting on Telefonica", "Telnet called back, confirmed") without pretending
-- the whole issue is resolved.
--
-- What each company needs is different, so the fields are company-specific:
--   * Telnet — the technician here who we reported the issue to, and the
--     technician who reported back that the fix was good. Both are people from
--     our own team, so both reference the `users` table.
--   * Telefonica — only the ticket id in their own system; they assign their
--     own technician and we have no name for them.
CREATE TABLE IF NOT EXISTS issue_support_requests (
    id                      bigserial PRIMARY KEY,
    created_at              timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_at              timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    issue_id                bigint NOT NULL REFERENCES issues ON DELETE CASCADE,
    company                 text NOT NULL CHECK (company IN ('telnet', 'telefonica')),
    status                  text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'solved')),

    -- Telnet only. Nullable on purpose: users are deactivated rather than
    -- deleted, but if one is ever removed the row must survive with the
    -- handover still on record instead of the deletion being rejected.
    telnet_technician_id    bigint REFERENCES users ON DELETE SET NULL,
    telnet_confirmed_by_id  bigint REFERENCES users ON DELETE SET NULL,

    -- Telefonica only: their own ticket reference, free text because they do
    -- not commit to one format.
    telefonica_ticket_id    text,

    notes                   text NOT NULL DEFAULT '',
    created_by              bigint REFERENCES users ON DELETE SET NULL,
    version                 integer NOT NULL DEFAULT 1,

    -- Keeps the company-specific fields coherent: a Telnet handover can never
    -- carry a Telefonica ticket id, and vice versa. (The requirement that a
    -- Telnet handover names a technician is enforced in Go, not here — see
    -- ValidateSupportRequests — so that deactivating the user later can't
    -- fail this check.)
    --
    -- Note that telnet_confirmed_by_id is deliberately NOT pinned on a Telnet
    -- row: it is one of the two things a Telnet handover needs.
    CONSTRAINT issue_support_requests_company_fields CHECK (
        (company = 'telnet'     AND telefonica_ticket_id IS NULL) OR
        (company = 'telefonica' AND telefonica_ticket_id IS NOT NULL
                                   AND telnet_technician_id IS NULL
                                   AND telnet_confirmed_by_id IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS issue_support_requests_issue_idx   ON issue_support_requests (issue_id);
CREATE INDEX IF NOT EXISTS issue_support_requests_company_idx ON issue_support_requests (company);
CREATE INDEX IF NOT EXISTS issue_support_requests_status_idx  ON issue_support_requests (status);

-- The same Telefonica ticket can never be logged twice on the same issue —
-- that is nearly always a copy/paste slip, and a duplicated reference makes it
-- impossible to tell which handover is actually being chased. The same ticket
-- on a *different* issue is fine (the reference belongs to the issue, not the
-- company), and NULLs are excluded so Telefonica's own uniqueness rule does not
-- constrain the Telnet rows.
CREATE UNIQUE INDEX IF NOT EXISTS issue_support_requests_telefonica_ticket_key
    ON issue_support_requests (issue_id, telefonica_ticket_id)
    WHERE telefonica_ticket_id IS NOT NULL;
