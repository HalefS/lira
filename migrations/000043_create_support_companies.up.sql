-- The external companies an issue can be handed over to.
--
-- Until now the set was fixed in code and in a CHECK constraint: Telnet and
-- Telefónica, each with its own bespoke fields. That made adding a third company
-- a migration plus a code change. This table turns the set into configuration a
-- manager can edit in Settings, while the two existing companies and every
-- handover already recorded against them stay exactly as they are.
--
-- `slug` is the stable key stored on issue_support_requests.company. It is what
-- history is matched on, so renaming a company changes only `name` -- the slug
-- never moves and no handover has to be rewritten.
--
-- `field_kind` says which fields a handover to that company carries:
--   * technician -- Telnet's two names (reported to, reported fixed by)
--   * ticket     -- Telefónica's own ticket id
--   * reference  -- the generic case for companies added later: one free-text
--                   reference (their ticket, case or job number)
--
-- Companies are deactivated rather than deleted. A deleted row would either
-- orphan its handovers or take them with it; deactivating keeps the history and
-- simply stops the company being offered on new handovers.
CREATE TABLE IF NOT EXISTS support_companies (
    id         bigserial PRIMARY KEY,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    slug       text NOT NULL UNIQUE,
    name       citext NOT NULL UNIQUE,
    color      text NOT NULL DEFAULT 'blue'
               CHECK (color IN ('blue', 'teal', 'amber', 'green', 'red', 'gray')),
    field_kind text NOT NULL DEFAULT 'reference'
               CHECK (field_kind IN ('technician', 'ticket', 'reference')),
    active     boolean NOT NULL DEFAULT true,
    created_by bigint REFERENCES users(id) ON DELETE SET NULL
);

-- The two companies that already have handovers. Their slugs are exactly the
-- values stored in issue_support_requests.company today, so this seeds the
-- catalog without touching a single existing row.
INSERT INTO support_companies (slug, name, color, field_kind) VALUES
    ('telnet',     'Telnet',     'blue',  'technician'),
    ('telefonica', 'Telefónica', 'amber', 'ticket')
ON CONFLICT (slug) DO NOTHING;

-- The generic reference used by companies added later. Telefónica keeps its own
-- ticket column and Telnet its technician columns, so neither built-in's data
-- has to be migrated.
ALTER TABLE issue_support_requests
    ADD COLUMN IF NOT EXISTS reference text;

-- The old CHECK pinned the company to the two hardcoded values; the company
-- fields rule was written around those same two names. Both are replaced below.
ALTER TABLE issue_support_requests
    DROP CONSTRAINT IF EXISTS issue_support_requests_company_check,
    DROP CONSTRAINT IF EXISTS issue_support_requests_company_fields;

ALTER TABLE issue_support_requests
    ADD CONSTRAINT issue_support_requests_company_fkey
        FOREIGN KEY (company) REFERENCES support_companies(slug)
        ON UPDATE CASCADE ON DELETE RESTRICT;

-- Coherence between the company and the fields its handover carries.
--
-- A CHECK cannot read support_companies.field_kind, so the rule is written per
-- kind and the two built-in slugs stand in for their kinds. A company added
-- later is always the generic 'reference' kind, which is the third branch.
ALTER TABLE issue_support_requests ADD CONSTRAINT issue_support_requests_company_fields CHECK (
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

-- The same reference can never be logged twice on the same issue. The rule is
-- per issue rather than per company, because a repeated reference is almost
-- always a copy/paste slip and makes it impossible to tell which handover is
-- being chased.
CREATE UNIQUE INDEX IF NOT EXISTS issue_support_requests_reference_key
    ON issue_support_requests (issue_id, reference)
    WHERE reference IS NOT NULL;
