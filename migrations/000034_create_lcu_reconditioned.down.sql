-- Drops the reconditioned pool ledger and the resting-section index.
--
-- Index first, then table, for the reason 000031's down migration gives.
--
-- What this loses is every reader that has ever been credited into the pool. It does
-- NOT lose anything about any trial: lcu_units and lcu_tests are untouched, so every
-- verdict, every day and every note survives, and re-running the up migration
-- re-derives the pool from the passed trials that are still present.
--
-- So this is the cheaper of the two LCU rollbacks, and the loss is bounded in a way
-- 000032's is not. 000032's down migration loses the EXCLUSIONS, and re-running the up
-- migration then puts EVERY account back on the rota -- a rolled-back deploy there
-- publishes the whole user list. Re-running this one gives back the pool those same
-- passed trials justify, which is the truth.
--
-- The one thing not recoverable: a credit whose trial row was deleted while the ledger
-- existed. That credit carried serial (so it still counted correctly) but its back-
-- pointer to the trial, which had already gone, and its counted_at timestamp. The
-- reader stays in the count on re-apply; the record of when it was credited does not.
--
-- The loss is therefore "the pool falls back to what the surviving trial rows can
-- prove", which is a defensible place to land and worth saying plainly rather than
-- discovering during a rollback.
DROP INDEX IF EXISTS lcu_units_rested_idx;
DROP INDEX IF EXISTS lcu_reconditioned_counted_idx;
DROP INDEX IF EXISTS lcu_reconditioned_serial_key;
DROP TABLE IF EXISTS lcu_reconditioned;
