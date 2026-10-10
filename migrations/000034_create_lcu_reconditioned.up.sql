-- The pool of readers that came through a trial with every day passing.
--
-- This is a LEDGER rather than a count(*) over lcu_units, and the reason is specific
-- enough to be worth spelling out. lcu_units rows are TRIALS, not READERS: 000019
-- deliberately left `serial` non-unique so a re-tested reader keeps its own history,
-- which means one physical reader trialled twice and passed twice is two rows in
-- lcu_units. Counting those rows says there are two good readers when there is one.
--
-- So the count is wrong on the FIRST re-trial, not eventually wrong, and 000019 says
-- re-testing is a supported thing to do. That is the whole argument for the table.
--
--
-- WHY IT IS KEYED ON serial AND NOT ON unit_id
--
-- Because the pool counts readers. unit_id would still double-count a re-trialled
-- reader, since each trial is its own row with its own id. Keying on the serial means
-- the physical thing in the cupboard is the thing being counted, which is what a
-- manager reaching for a spare reader is asking about.
--
-- It also means redeployment, when it arrives, is a small change rather than a
-- redesign. Assigning a reader to a room produces a NEW unit row for the SAME serial;
-- a unit_id-keyed ledger would have two rows claiming one reader and no way to retire
-- the old claim. With serial as the key it is one nullable column and one predicate on
-- the count query. `retired_at` is deliberately NOT added now: an always-NULL column
-- with no writer is a claim about the future that nothing maintains.
--
--
-- WHY unit_id IS SET NULL RATHER THAN CASCADE
--
-- Deleting a trial row is tidying up paperwork. It does not scrap a reader, and it
-- does not take one out of a cupboard. If the ledger cascaded, the pool would shrink
-- because somebody deleted a duplicate row, and the number would stop matching the
-- physical stock -- which is the one thing a stock number must never do. The reader is
-- in the cupboard either way, and `serial` is snapshotted here precisely so the entry
-- still reads correctly once the trial row behind it is gone. Same argument as
-- 000018's treatment of issue_id.
--
--
-- WHY THERE IS NO balance_after
--
-- 000018 needs one because stock there is a mutable integer a movement has to be
-- reconciled against. Here the count IS the row count, so a stored balance could only
-- ever disagree with count(*) without adding anything. count(*) is the single source
-- of truth and cannot drift.
--
--
-- WHO IS CREDITED
--
-- added_by, copied from the trial's own added_by -- the person who actually put the
-- reader on trial and answered its days. It is NOT the user whose page load happened
-- to run the sweep: ResolveDue fires on whichever request happens to sweep, so
-- attributing the credit to that user would put a cupboard decision in a colleague's
-- name. 000019's added_by is SET NULL on user deletion, so this follows suit.
CREATE TABLE IF NOT EXISTS lcu_reconditioned (
    id         bigserial PRIMARY KEY,
    counted_at timestamp(0) with time zone NOT NULL DEFAULT NOW(),
    -- The reader's serial, snapshotted rather than joined through. See above.
    serial     text NOT NULL CHECK (length(trim(serial)) > 0),
    -- The trial that earned the place. NULL once that trial has been deleted, which
    -- does NOT take the reader out of the pool.
    unit_id    bigint UNIQUE REFERENCES lcu_units ON DELETE SET NULL,
    added_by   bigint REFERENCES users ON DELETE SET NULL
);

-- The pool holds readers, so the reader is the key. Normalised the way
-- lcu_units_serial_idx already normalises it, so "ABC-1" and " abc-1 " are one reader
-- rather than two -- and so ResolveDue's ON CONFLICT can infer this expression index.
CREATE UNIQUE INDEX IF NOT EXISTS lcu_reconditioned_serial_key
    ON lcu_reconditioned (lower(trim(serial)));

-- Newest first, for the resting section and the "added to the pool" history.
CREATE INDEX IF NOT EXISTS lcu_reconditioned_counted_idx
    ON lcu_reconditioned (counted_at DESC, id DESC);

-- The resting section's query: resolved units, newest verdict first.
--
-- Partial, because it is only ever asked about units that are NOT active, and the
-- active list is the common case. The `status <> 'active'` predicate is deliberately
-- the CHECK-constrained column rather than `resolved_at IS NOT NULL`: the two are
-- equivalent today and resolved_at is an unconstrained timestamp that any future
-- write could set for some other reason, and a section that appears because a
-- timestamp was touched by something unrelated is a section nobody trusts.
--
-- id DESC is in the key because resolved_at is timestamp(0): every unit resolved by
-- the SAME sweep shares one second, and without it page boundaries inside one sweep
-- are non-deterministic -- "load more" would drop or repeat rows.
CREATE INDEX IF NOT EXISTS lcu_units_rested_idx
    ON lcu_units (resolved_at DESC, id DESC)
    WHERE status <> 'active';

-- The backfill, so an installation that already has passed trials starts with the
-- right number rather than an empty pool that fills in over the next week.
--
-- DISTINCT ON the normalised serial with the EARLIEST resolved_at: the first pass is
-- what put the reader in the cupboard, and a re-trial years later does not make it a
-- second one.
INSERT INTO lcu_reconditioned (serial, unit_id, added_by)
SELECT DISTINCT ON (lower(trim(u.serial))) u.serial, u.id, u.added_by
FROM lcu_units u
WHERE u.status = 'passed'
ORDER BY lower(trim(u.serial)), u.resolved_at ASC, u.id ASC
ON CONFLICT (lower(trim(serial))) DO NOTHING;
