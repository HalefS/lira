-- The fields a handover asks for become configuration rather than code.
--
-- Support companies already have stable slugs, and every handover points at one.
-- This migration keeps that key and adds a per-company field catalogue: a
-- manager can choose the information each company needs, give it a label, and
-- say whether it is required. The two built-in companies are seeded from the
-- fields they always had, and every handover's existing values are copied into
-- the new JSONB bag, so no history is rewritten or lost.
CREATE TABLE IF NOT EXISTS support_company_fields (
    id           bigserial PRIMARY KEY,
    company_slug text NOT NULL
                 REFERENCES support_companies(slug)
                 ON UPDATE CASCADE ON DELETE RESTRICT,
    field_key    text NOT NULL,
    label        text NOT NULL,
    field_type   text NOT NULL
                 CHECK (field_type IN ('text', 'name', 'confirmation', 'reference', 'date', 'number')),
    required     boolean NOT NULL DEFAULT false,
    active       boolean NOT NULL DEFAULT true,
    position     integer NOT NULL DEFAULT 0,
    UNIQUE (company_slug, field_key)
);

CREATE INDEX IF NOT EXISTS support_company_fields_active_idx
    ON support_company_fields (company_slug, active, position);

-- Telnet's two names and Telefónica's ticket are the original fields. Their
-- keys are stable so old handovers keep matching them after the labels are
-- edited in Settings. `confirmation` stays required when a handover is solved;
-- that rule is now carried by the field type rather than a fixed SQL CHECK.
INSERT INTO support_company_fields
    (company_slug, field_key, label, field_type, required, active, position)
VALUES
    ('telnet', 'technician', 'Reported to', 'name', true, true, 1),
    ('telnet', 'confirmed_by', 'Reported fixed by', 'confirmation', false, true, 2),
    ('telefonica', 'ticket_id', 'Ticket ID', 'reference', true, true, 1)
ON CONFLICT (company_slug, field_key) DO NOTHING;

-- Companies added in Settings already used the generic reference column, so
-- they keep exactly one field with the same meaning.
INSERT INTO support_company_fields
    (company_slug, field_key, label, field_type, required, active, position)
SELECT slug, 'reference', 'Reference', 'reference', true, true, 1
FROM support_companies
WHERE slug NOT IN ('telnet', 'telefonica')
ON CONFLICT (company_slug, field_key) DO NOTHING;

ALTER TABLE issue_support_requests
    ADD COLUMN IF NOT EXISTS fields jsonb NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS reference_summary text,
    ADD COLUMN IF NOT EXISTS primary_reference text;

-- Copy the old, company-specific columns into the configured shape. Existing
-- rows stay in place; only their values get a second, company-independent home.
UPDATE issue_support_requests s
SET fields = CASE
        WHEN s.company = 'telnet' THEN
            jsonb_strip_nulls(jsonb_build_object(
                'technician', s.telnet_technician,
                'confirmed_by', s.telnet_confirmed_by))
        WHEN s.company = 'telefonica' THEN
            jsonb_strip_nulls(jsonb_build_object('ticket_id', s.telefonica_ticket_id))
        ELSE
            jsonb_strip_nulls(jsonb_build_object('reference', s.reference))
    END,
    reference_summary = COALESCE(
        NULLIF(btrim(s.reference), ''),
        NULLIF(btrim(s.telefonica_ticket_id), ''),
        NULLIF(btrim(s.telnet_technician), ''),
        NULL),
    primary_reference = CASE
        WHEN s.company = 'telefonica' THEN NULLIF(btrim(s.telefonica_ticket_id), '')
        WHEN s.company NOT IN ('telnet', 'telefonica') THEN NULLIF(btrim(s.reference), '')
        ELSE NULL
    END
WHERE s.fields = '{}'::jsonb AND s.reference_summary IS NULL;

-- The old CHECK names the old columns and cannot express configurable fields.
-- Required values are now checked in the data layer against the company's
-- current definitions; the database keeps the durable uniqueness rule below.
ALTER TABLE issue_support_requests
    DROP CONSTRAINT IF EXISTS issue_support_requests_company_fields;

CREATE UNIQUE INDEX IF NOT EXISTS issue_support_requests_primary_reference_key
    ON issue_support_requests (issue_id, lower(primary_reference))
    WHERE primary_reference IS NOT NULL AND btrim(primary_reference) <> '';
