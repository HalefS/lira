-- Drops the room inventory. The runs are derived from nothing -- the room numbers
-- themselves are the data, and every apartment issue keeps its own stored
-- location string exactly as it was, so nothing outside this table depends on it.
--
-- The rooms seeded from existing apartment issues go with it. Re-running the up
-- migration recreates them from the same issues, because the seed reads
-- issues.location rather than any column of this table.
DROP TABLE IF EXISTS rooms;
