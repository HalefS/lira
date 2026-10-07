-- Index first, then table, for the reason 000031's down migration gives.
--
-- The cost here is WORSE than 000029's and worth stating plainly rather than
-- discovering later during a rollback. 000029 drops the rota and can say "configuration,
-- nothing to re-derive", because what it loses comes back empty.
--
-- THIS one loses the exclusions specifically. Re-running the up migration restores
-- everybody to the rota, so a rolled-back deployment does not produce an empty
-- noticeboard -- it produces a FULL one, printing every account in the installation to
-- anybody who loads the public page. Before running this, note who was excluded.
DROP INDEX IF EXISTS rota_members_included_idx;
DROP TABLE IF EXISTS rota_members;
