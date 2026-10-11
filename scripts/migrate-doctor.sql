-- LIRA migration doctor. Read-only: SELECTs only, no writes, safe to run on a live
-- database while the server is up.
--
-- Answers one question: what does schema_migrations claim, and what is actually
-- there? When those disagree, this is the whole diagnosis.

\pset border 2

\echo ''
\echo '=== 1. What the bookkeeping says ==='
SELECT version, dirty FROM schema_migrations;
--   dirty = false  -> migrate believes it is at `version`. Normal.
--   dirty = true   -> a migration FAILED partway. `version` is the one that failed,
--                     which is NOT necessarily the newest one. Every migrate command
--                     refuses until you `force`.

\echo ''
\echo '=== 2. Objects each recent migration creates ==='
SELECT m.version,
       CASE WHEN m.creates LIKE '%users:rota_position%'
                 AND EXISTS (SELECT 1 FROM information_schema.columns
                              WHERE table_name='users' AND column_name='rota_position')
            THEN 'PRESENT'
            WHEN m.creates LIKE '%lcu_reconditioned%'
                 AND EXISTS (SELECT 1 FROM information_schema.tables
                              WHERE table_name='lcu_reconditioned')
            THEN 'PRESENT'
            WHEN m.creates LIKE '%shift_week_cells%'
                 AND EXISTS (SELECT 1 FROM information_schema.tables
                              WHERE table_name='shift_week_cells')
            THEN 'PRESENT'
            ELSE 'absent'
       END AS actually_there
FROM (VALUES (33, 'users:rota_position'),
             (34, 'lcu_reconditioned'),
             (35, 'shift_week_cells')) AS m(version, creates)
ORDER BY m.version;
--   PRESENT above the recorded version means the schema is AHEAD of the bookkeeping:
--   the migration was applied by something other than migrate, or the bookkeeping was
--   reset. That is the situation the "already exists" error is reporting.

\echo ''
\echo '=== 3. Partial migrations (object missing but sibling present) ==='
SELECT '000033 constraint' AS object,
       EXISTS (SELECT 1 FROM pg_constraint
                WHERE conrelid='users'::regclass
                  AND conname='users_rota_position_positive') AS present
UNION ALL
SELECT '000035 freeze trigger',
       EXISTS (SELECT 1 FROM pg_trigger
                WHERE tgrelid='shift_week_cells'::regclass AND NOT tgisinternal)
UNION ALL
SELECT '000035 guard function',
       EXISTS (SELECT 1 FROM pg_proc
                WHERE proname='shift_week_cells_freeze_guard');

\echo ''
\echo '=== 4. Is this the database the app is talking to? ==='
SELECT current_database() AS this_database,
       current_user     AS this_user,
       inet_server_addr()::text AS host,
       inet_server_port() AS port;
--   Compare the database name against the -db-dsn / LIRADB_DSN the server was
--   started with. Migrating a different database than the one the app reads is the
--   other way to get a version and a schema that make no sense together.

\echo ''
\echo '=== 5. citext must exist BEFORE any migration runs ==='
SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname='citext') AS citext_present;
--   Six migrations use citext, from 000001 onward, and NO migration creates the
--   extension. `make db/setup` does. False here means the database was created
--   without it and migration 000001 would fail.
