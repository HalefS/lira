-- Catalog of Melia Connecta Agent names, managed from the admin panel —
-- same shape as issue_types / departments.
CREATE TABLE IF NOT EXISTS connecta_agents (
    id         bigserial PRIMARY KEY,
    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    name       citext UNIQUE NOT NULL,
    created_by bigint REFERENCES users ON DELETE SET NULL
);

-- Which agent reported the issue to us, and which agent we told once it
-- was fixed. Both optional (an issue may still be Pending, so there's no
-- "confirmed" agent yet) and nullable so existing issues are unaffected.
-- Deliberately NOT surfaced in any issue list/table/dashboard — only the
-- add/edit issue form reads or writes these columns.
ALTER TABLE issues
    ADD COLUMN IF NOT EXISTS reported_by_agent  text,
    ADD COLUMN IF NOT EXISTS confirmed_by_agent text;
