-- Drops absences.
--
-- Indexes first, then the table, for the reason 000029's down migration gives:
-- the indexes belong to the table and naming them after it keeps the two halves
-- of this file in the same order as the table's own creation.
--
-- Nothing outside this table references it, and the honest cost is worth stating
-- rather than discovering later. This migration seeds nothing, so re-running the
-- up migration gives an empty set of absences and every vacation, sick day and
-- training course anyone recorded is gone with no way to recover it. That is a
-- different bargain from 000029's, which can say "configuration, nothing to
-- re-derive"; a recorded absence is a statement somebody made about a named
-- colleague on a specific date, and it is not derivable from the rota or from
-- anything else in this database.
DROP INDEX IF EXISTS absences_user_idx;
DROP INDEX IF EXISTS absences_window_idx;
DROP TABLE IF EXISTS absences;