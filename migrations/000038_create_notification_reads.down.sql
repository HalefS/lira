-- Read state is not recoverable, and does not need to be: it records that a
-- person acknowledged a derived item, and the item is recomputed from the
-- domain tables on the next request. Everything reads as unread afterwards.
DROP TABLE IF EXISTS notification_reads;
