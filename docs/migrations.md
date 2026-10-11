# Database migrations

**Status:** Current — this is the working procedure
**Scope:** How schema changes reach a database in LIRA, and what to do when the
migration tool and the database disagree.

---

## 1. The rule

**Apply migrations with `migrate`. Never with `psql`.**

```sh
make db/migrations/new name=add_widgets    # writes 000036_*.up.sql and .down.sql
make db/migrations/up                       # applies them, in order
make db/migrations/status                   # where am I, and is the database healthy
```

`make db/migrations/up` and `make db/migrations/down` are thin wrappers around the
`migrate` CLI pointed at `${LIRADB_DSN}` from `.envrc`. The application **never
runs migrations itself** — there is no migration step in `cmd/api`, deliberately,
so that starting the server can never change the shape of a database.

`psql` is for looking at things. Reading the schema, counting rows, checking a
constraint, running a `SELECT` to find out what is actually true. All of that is
encouraged, and `make db/psql` is there for it.

---

## 2. Why the rule exists, told through a real incident

This is not a style preference. It is the only thing standing between a
migration and a database that cannot be migrated again.

Migrations `000033`, `000034` and `000035` were each applied to the development
database by hand, with:

```sh
psql -d lira -f migrations/000035_create_shift_week_cells.up.sql
```

Every one of those commands **succeeded**, and the schema was correct
afterwards. That is the trap: the thing you are trying to do — get the schema
into a particular shape — works perfectly.

But `schema_migrations` is written **only** by the `migrate` CLI. `psql -f`
creates tables, columns and triggers; it never learns they happened. So the
database was left in a state where the schema said 35 and the bookkeeping said
32.

The next `make db/migrations/up` did what it was told. It read 32, believed it,
and tried to apply `000033`, whose first statement is:

```sql
ALTER TABLE users ADD COLUMN rota_position integer;
```

That column was already there. The statement failed, and `migrate` did the only
thing it could with a failed migration: it recorded the attempt and stopped.

```
schema_migrations:  version = 33,  dirty = true
```

From that point **no migration command works at all** — not `up`, not `down`, not
`force` by accident. Every one of them refuses with:

```
error: Dirty database version 33. Fix and force version.
```

And the error names migration 33, which is the confusing part. The person running
it had just added 35, sees an error about 33, and concludes the wrong thing about
which migration is broken.

**`schema_migrations` holds exactly one row** — `(version bigint, dirty boolean)`
— and nothing about *how* the database reached that version. The schema and the
bookkeeping are two independent records of the same history, and only one of them
is written by hand.

---

## 3. Creating a migration

```sh
make db/migrations/new name=add_widgets
```

This writes `migrations/000036_add_widgets.up.sql` and `.down.sql`. The sequence
number is global and shared with every other developer, so **read the last
number before choosing one** and never reuse a retired number.

A `.up.sql` in this codebase is expected to argue its own decisions in comments
— why this column is nullable, what a rollback costs, which assumption would
have to break for the design to be wrong. That is the house style and it is
load-bearing: `000035` and `000029` disagree about whether `shift_id` may be
NULL, and the comment is the only place that disagreement is resolved.

A `.down.sql` must say what a rollback **loses**, not just that it drops a
column. `000035`'s down migration states plainly that rolling back restores the
bug this migration was written to fix, and that per-week edits made since deploy
are **not** recoverable, because the application does not keep the pattern's
history.

---

## 4. A dirty database: diagnosing it

```sh
make db/migrations/status
```

If this prints `dirty`, nothing else will work until it is cleared. Two commands
tell you what is actually true:

```sh
migrate -path ./migrations -database ${LIRADB_DSN} version
psql ${LIRADB_DSN} -c 'SELECT version, dirty FROM schema_migrations'
```

Both report the same single row. `version` is the migration that **failed**,
not the last one that succeeded, which is why it is often lower than the number
you were working on.

---

## 5. A dirty database: recovering

`force` writes the version number. **It runs no SQL.**

```sh
migrate -path ./migrations -database ${LIRADB_DSN} force 35
```

Because it executes nothing, it also **verifies nothing**. Forcing a version is a
claim — "everything up to here is applied" — and if that claim is false, the
missing migration is now permanently invisible: the tool thinks it ran.

So **check the schema before forcing, every time.** For the incident in §2 that
meant confirming, against the migration files, that every object was present:

```sql
-- 000033
SELECT column_name, data_type FROM information_schema.columns
  WHERE table_name = 'users' AND column_name = 'rota_position';
SELECT conname FROM pg_constraint
  WHERE conrelid = 'users'::regclass AND conname = 'users_rota_position_positive';

-- 000034
SELECT column_name FROM information_schema.columns
  WHERE table_name = 'lcu_reconditioned' ORDER BY ordinal_position;

-- 000035
SELECT column_name FROM information_schema.columns
  WHERE table_name = 'shift_week_cells' ORDER BY ordinal_position;
SELECT indexname FROM pg_indexes WHERE tablename = 'shift_week_cells';
SELECT tgname FROM pg_trigger
  WHERE tgrelid = 'shift_week_cells'::regclass AND NOT tgisinternal;
```

Only when all of that is present, and only then:

```sh
migrate -path ./migrations -database ${LIRADB_DSN} force 35
make db/migrations/status      # expect: 35, not dirty
make db/migrations/up          # expect: no change
```

### If the schema is *not* complete

Then the honest sequence is: force back to the last version you can **prove** was
applied, let `migrate up` replay the rest, and let it fail again on whatever is
genuinely missing.

```sh
migrate -path ./migrations -database ${LIRADB_DSN} force 32
make db/migrations/up
```

This works because the objects the failed migration *did* create are usually
`CREATE TABLE IF NOT EXISTS`-shaped or otherwise re-runnable. Where they are not,
the half-applied statement has to be undone by hand first — check the migration's
own comment, because that is where this is argued.

### Never force to a version you have not looked at

Forcing past a gap does not fix the gap. It converts "loud failure" into "quiet
missing column", and the quiet version is found weeks later by a feature that
does not work.

---

## 6. Testing a migration by hand

Writing and debugging a migration against a live database is normal, and the rule
in §1 does not forbid it. What it forbids is **applying a migration by hand and
then continuing as though it had been applied**.

During development it is fine to:

- run one statement at a time to see what a constraint actually rejects;
- test a trigger by inserting the rows that should fire it;
- read the schema, count rows, check an index.

Before the **next** migration is written, the database must be back in a state
`migrate` agrees with. Either replay it properly, or — if the hand-applied
version was correct and you want to keep it — verify the schema as in §5 and
`force` to that version, in that order, deliberately.

The failure mode is always the same: the hand-applied schema looks right, so
nothing prompts anyone to reconcile the bookkeeping, and the collision surfaces
later as a dirty database on somebody else's machine.

---

## 7. Seeds are not migrations

`migrations/seed.sql` and `migrations/seed_lcu_demo.sql` are applied with
`psql`, on purpose:

```sh
make db/seed
```

They are demo data, they are not versioned, and they are not supposed to be
re-runnable in order against a schema that has already moved on. They live in
`migrations/` for convenience and nothing else — do not read their presence
there as a precedent for applying `0000NN_*.up.sql` the same way.

---

## 8. References

- `migrate` CLI: <https://github.com/golang-migrate/migrate> — a copy of the
  upstream README is kept at [`golang-migrate.md`](./golang-migrate.md) for the
  flags and driver details this document does not cover.
- `Makefile` targets `db/migrations/{new,up,down,status}`.
- Migrations to read before writing one: `000029` (nullable columns),
  `000031` (date columns), `000032` (tables with implicit membership),
  `000035` (per-week data, and why a trigger needs `TG_OP`).
