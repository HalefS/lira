-- LCU demo fixture. DEV ONLY -- opt in, never run by the numbered migrations.
--
--     psql "${LIRADB_DSN}" -f ./migrations/seed_lcu_demo.sql
--
-- Why this exists rather than living in seed.sql: the LCU page's resting history and
-- its reconditioned pool are impossible to reach by hand. A trial takes seven days,
-- so nobody can create one that finished last Tuesday and then look at what the page
-- does with it. This fixture writes trials in the past instead, which is the only way
-- to see the feature at all without waiting a week.
--
-- THE INTERESTING PART: every historical trial below is inserted with status 'active'
-- and a window that has already elapsed, and its seven days pre-recorded. It is NOT
-- inserted as 'passed', and lcu_reconditioned is NOT written directly.
--
-- That is deliberate. ResolveDue does the resolving and does the crediting, so the
-- first request to GET /v1/lcu/resting converts these rows, fills the pool, and
-- returns a non-empty newly_reconditioned -- which is what makes the UI's "+N readers
-- added to the pool" toast appear. A fixture that inserted 'passed' rows and a
-- matching ledger would render the same picture while proving nothing about the code
-- that actually runs, and the moment the sweep changed, the screenshot would still be
-- the old one.
--
-- Re-running is safe: the serial guard below makes every insert a no-op once the
-- history exists. To see the sweep happen again, delete the historical rows first:
--
--     DELETE FROM lcu_units WHERE serial IN (SELECT unnest(ARRAY[
--       '6650tr9','88231pt','10934kk','5510zl','7301bx','2245dp','9918hg','4482qw']);
--
-- Nothing here is referenced by id, so it works on any database. Users are looked up
-- by email and the fixture inserts whichever of them exist.
BEGIN;

-- ── Two readers currently ON TRIAL ────────────────────────────────────────────────
-- Three days into a seven-day window, with two days recorded, so the page opens on
-- live work rather than on an empty list.
INSERT INTO lcu_units (serial, added_by, starts_on, ends_on, status, note, window_days)
SELECT v.serial, u.id, CURRENT_DATE - 2, CURRENT_DATE + 4, 'active', v.note, 7
FROM (VALUES
    ('6102vr4', 'Room 118 main door'),
    ('3390dp7', 'Room 204 gym side entrance')
) AS v(serial, note)
CROSS JOIN LATERAL (
    SELECT id FROM users WHERE email = 'review@melia.com' LIMIT 1
) u
WHERE NOT EXISTS (SELECT 1 FROM lcu_units lu WHERE lu.serial = v.serial);

-- The days already answered on those two.
INSERT INTO lcu_tests (unit_id, day, result, logged_by)
SELECT lu.id, d::date, 'pass', lu.added_by
FROM lcu_units lu
CROSS JOIN LATERAL generate_series(CURRENT_DATE - 2, CURRENT_DATE - 1, interval '1 day') d
WHERE lu.serial IN ('6102vr4', '3390dp7')
ON CONFLICT (unit_id, day) DO NOTHING;

-- ── Four readers that PASSED, trialled by the reviewer account ────────────────────
-- These are the ones that will land in the pool as "yours".
--
-- NOTE ON THE serial GUARD: NOT EXISTS below makes every insert a no-op when a serial
-- is already present, which is right for re-runnability and quietly wrong when the
-- database already has LCU data of its own -- the row is skipped and the fixture
-- silently contributes one fewer reader than it appears to. That is not hypothetical:
-- an earlier draft used 4775yr6, which this installation already had, and the guard
-- swallowed it without a word. If a count here looks wrong, SELECT serial FROM
-- lcu_units before assuming the fixture ran.
INSERT INTO lcu_units (serial, added_by, starts_on, ends_on, status, note, window_days)
SELECT v.serial, u.id, CURRENT_DATE - v.start_ago, CURRENT_DATE - v.start_ago + 6,
       'active', v.note, 7
FROM (VALUES
    ('6650tr9', 21, 'Room 112 main door'),
    ('88231pt', 34, 'Room 305 bedroom door'),
    ('10934kk', 48, 'Reception back office'),
    ('5510zl',  63, 'Room 417 main door')
) AS v(serial, start_ago, note)
CROSS JOIN LATERAL (
    SELECT id FROM users WHERE email = 'review@melia.com' LIMIT 1
) u
WHERE NOT EXISTS (SELECT 1 FROM lcu_units lu WHERE lu.serial = v.serial);

-- ── Two readers that PASSED, trialled by somebody else ────────────────────────────
-- So the pool total and "added by you" are different numbers, which is the whole
-- point of showing them as two cards.
INSERT INTO lcu_units (serial, added_by, starts_on, ends_on, status, note, window_days)
SELECT v.serial, u.id, CURRENT_DATE - v.start_ago, CURRENT_DATE - v.start_ago + 6,
       'active', v.note, 7
FROM (VALUES
    ('7301bx', 12, 'Room 208 main door'),
    ('2245dp', 27, 'Laundry entrance')
) AS v(serial, start_ago, note)
CROSS JOIN LATERAL (
    SELECT id FROM users WHERE email = 'fabio.garcia@melia.com' LIMIT 1
) u
WHERE NOT EXISTS (SELECT 1 FROM lcu_units lu WHERE lu.serial = v.serial);

-- ── Two readers that FAILED ──────────────────────────────────────────────────────
-- One with a single failing day and one that was never finished, because 000019 is
-- explicit that a day with no result is not a pass and those two failures have
-- different reasons to have been scrapped.
INSERT INTO lcu_units (serial, added_by, starts_on, ends_on, status, note, window_days)
SELECT v.serial, u.id, CURRENT_DATE - v.start_ago, CURRENT_DATE - v.start_ago + 6,
       'active', v.note, 7
FROM (VALUES
    ('9918hg', 15, 'Room 119 main door',        'review@melia.com'),
    ('4482qw', 41, 'Room 231 service door',     'fabio.garcia@melia.com')
) AS v(serial, start_ago, note, email)
CROSS JOIN LATERAL (
    SELECT id FROM users WHERE email = v.email LIMIT 1
) u
WHERE NOT EXISTS (SELECT 1 FROM lcu_units lu WHERE lu.serial = v.serial);

-- ── The days themselves ──────────────────────────────────────────────────────────
-- Everything passes, except one day on 9918hg which failed, and the last three days
-- of 4482qw which were never answered at all.
INSERT INTO lcu_tests (unit_id, day, result, logged_by)
SELECT lu.id, d::date, 'pass', lu.added_by
FROM lcu_units lu
CROSS JOIN LATERAL generate_series(lu.starts_on, lu.ends_on, interval '1 day') d
WHERE lu.serial IN ('6650tr9', '88231pt', '10934kk', '5510zl',
                    '7301bx', '2245dp', '9918hg')
  AND NOT (lu.serial = '9918hg' AND d::date = lu.starts_on + 2)
ON CONFLICT (unit_id, day) DO NOTHING;

INSERT INTO lcu_tests (unit_id, day, result, logged_by)
SELECT lu.id, lu.starts_on + 2, 'fail', lu.added_by
FROM lcu_units lu
WHERE lu.serial = '9918hg'
ON CONFLICT (unit_id, day) DO NOTHING;

INSERT INTO lcu_tests (unit_id, day, result, logged_by)
SELECT lu.id, d::date, 'pass', lu.added_by
FROM lcu_units lu
CROSS JOIN LATERAL generate_series(lu.starts_on, lu.starts_on + 3, interval '1 day') d
WHERE lu.serial = '4482qw'
ON CONFLICT (unit_id, day) DO NOTHING;

COMMIT;
