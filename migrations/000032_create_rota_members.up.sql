-- Which accounts appear on the team rota, so the grid is no longer obliged to print
-- every user account that has ever existed. Test accounts, staff of another property
-- and accounts belonging to nobody in particular were all appearing as rows reading
-- "Not on this rota", which made a real rota hard to read and the accounting easy to
-- get wrong.
--
-- A MISSING ROW MEANS THE MEMBER IS ON THE ROTA. That one decision is the whole
-- design, and it is what makes this migration safe to deploy: the file below inserts
-- NOTHING, so an installation that has never had a roster carries no rows and reads
-- as "everybody on", which is exactly the behaviour it had before this migration
-- existed. There is no backfill and there must never be one.
--
-- The inverse default -- a bare set table where an absent row means NOT on the rota --
-- is the obvious design and the wrong one here. An empty table would then mean nobody
-- is rostered, so the deploy would blank a PUBLIC noticeboard. The only fix would be to
-- backfill every existing account, which obliges registerUserHandler to insert a row
-- for every future account forever, or the next person to sign up would silently fail
-- to appear on the board. That is a permanent write-path obligation bought for a
-- feature that does not need it.
--
-- included=false is kept as a ROW rather than DELETEd, which is redundant with the
-- absence of a row and is exactly the point. Absence means "nobody has decided yet";
-- false means "somebody decided, on this date, and it was this manager" (updated_at,
-- updated_by). Collapsing the two would throw away the only record that an exclusion
-- was deliberate -- the same argument migration 000029 makes about keeping a
-- deactivated member's row instead of dropping it.
--
-- NO VERSION COLUMN, unlike every other table in this schema, and the reasoning is
-- recorded here because the next person will otherwise add one by muscle memory.
-- absences.version exists because extending a holiday is an UPDATE of PART of a row --
-- a genuine read-modify-write that two managers can interleave. A roster write is a
-- PUT of the WHOLE set, exactly like PUT /v1/schedule/:user_id, so the server never
-- reads the current state to compute the new one and there is no partial row to
-- interleave with. Concurrency is handled by an exclusive table lock in the model
-- instead, for a reason a migration cannot express.
--
-- Nothing is seeded. As with 000029 there is no set of rota members every hotel shares:
-- who is on the team is the single most hotel-specific fact in this schema, and
-- inventing rows would put people on a board nobody chose.
CREATE TABLE IF NOT EXISTS rota_members (
    user_id    bigint PRIMARY KEY REFERENCES users ON DELETE CASCADE,

    -- NOT NULL DEFAULT true so that an omitted column can only ever mean "on" and can
    -- never be NULL, which would force a third answer into every reader. Almost always
    -- true or absent; false is the exception that records a decision.
    included   boolean NOT NULL DEFAULT true,

    created_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    updated_by bigint REFERENCES users ON DELETE SET NULL
);

-- Partial, because the included rows are a small subset of accounts: with a few hundred
-- staff and a rota of six, this is six index entries rather than a scan of every
-- exclusion ever recorded. Both readers need it -- the grid's semi-join probes on
-- (user_id, included), and the roster's own read filters on included.
--
-- There is deliberately NO index on user_id alone. The primary key already leads on it,
-- and that is also the index the ON DELETE CASCADE needs. Migration 000031 has to add
-- absences_user_idx only because its primary key is on id.
CREATE INDEX IF NOT EXISTS rota_members_included_idx
    ON rota_members (user_id) WHERE included;

COMMENT ON TABLE rota_members IS
    'Rota membership. A MISSING ROW MEANS INCLUDED, so an install with no rows here shows every account. A false row records a deliberate exclusion and is kept, not deleted, so the decision keeps its date and its author.';
COMMENT ON COLUMN rota_members.included IS
    'Whether this person is on the team rota. Absence of a row means true. False records a deliberate exclusion and is retained for its audit trail.';
