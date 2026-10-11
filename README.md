# LIRA

**Llana IT Reporting & Alerts** — a maintenance and operations application for
hotel technical departments. Issues, preventive maintenance, the team rota,
consumables, absence, and the daily and attendance reports that come out of all
of it.

One Go binary, one PostgreSQL database, one embedded single-file frontend. No
CDN, no build step at run time, no external services.

---

## Stack

| | |
|---|---|
| Backend | Go 1.22, `net/http` + [httprouter](https://github.com/julienschmidt/httprouter) |
| Database | PostgreSQL, [`lib/pq`](https://github.com/lib/pq), `citext` extension |
| Frontend | React, authored as JSX in one file, compiled with a pinned esbuild |
| Auth | Bearer tokens, one session per account |

There are no other runtime dependencies. The frontend has none at all: the
compiled JavaScript and CSS are embedded into the binary with `go:embed`.

---

## Quick start

Requires Go 1.22+ and PostgreSQL with the **`citext`** extension available.
Node is needed only to rebuild the frontend.

```sh
make db/setup          # create the database and enable citext  <- do this FIRST
make db/migrations/up  # apply migrations
make db/seed           # demo users
make ui                # compile cmd/api/ui/src/index.html -> cmd/api/ui/index.html
make run/api           # build and run on :4000
```

Then open <http://localhost:4000>.

> **`db/setup` is not optional.** Six migrations use `citext` — starting at
> `000001`, which puts it on `users.email` — and **no migration creates the
> extension**; `db/setup` does. A database created any other way fails at the
> first migration with `type "citext" does not exist`. Developed and tested
> against PostgreSQL 18.

`.envrc` holds `LIRADB_DSN` and every Makefile target reads it. `make help`
lists the targets; the ones that matter most are below.

---

## Commands

| Target | What it does |
|---|---|
| `make run/api` | Build and run the server |
| `make run/api/dev` | Run with no rate limit, in development mode |
| `make ui` | **Compile the frontend** — run after every edit to `cmd/api/ui/src/index.html` |
| `make audit` | `go mod tidy`, `go fmt`, `go vet`, `go test -race` |
| `make db/migrations/status` | Where the database is, and whether it is healthy |
| `make db/psql` | Open a psql session |

### The frontend is compiled, not interpreted

`cmd/api/ui/src/index.html` holds JSX. `cmd/api/ui/index.html` holds the
compiled output and **is committed**, so `go build` and every cross-compile
target need nothing but Go.

Editing the JSX without running `make ui` means `go build` succeeds and serves
the previous version — the change is not an error, it is simply not there yet.

---

## Migrations

**Apply migrations with `migrate`, never with `psql`.**

```sh
make db/migrations/new name=add_widgets
make db/migrations/status
make db/migrations/up
```

`psql -f migrations/0000NN.up.sql` applies a migration perfectly — every table,
column and trigger is created — and records nothing, because `schema_migrations`
is written only by the `migrate` CLI. The next `up` then re-applies a migration
whose objects already exist, fails, and leaves the database **dirty**: from then
on every migration command refuses, naming the migration that failed rather than
the one you were working on.

`make db/migrations/status` detects that state and explains the recovery;
`make db/migrations/up` refuses with the same explanation rather than passing a
bare "Fix and force version" back. Forcing a version **runs no SQL and verifies
nothing**, so check the schema first — see
[`docs/migrations.md`](./docs/migrations.md).

The application never migrates on startup. That is deliberate: starting the
server must not be able to change the shape of a database.

---

## Layout

```
cmd/api/            HTTP handlers, routing, middleware, the embedded UI
  ui/src/index.html   JSX source for the whole frontend
  ui/index.html       compiled output, committed
  ui/build.mjs        the compiler
internal/data/       models, queries, migrations-aware business rules
internal/report/     PDF, XLSX and attendance renderers
internal/validator/  input validation
migrations/          numbered .up.sql / .down.sql pairs, plus seed data
docs/                design and operations documents
```

`internal/data` is where the rules live. Anything that decides what a request
*means* — whether a rota week is closed, whether a cell is "off" or "inherit" —
belongs there rather than in a handler, so that two entry points cannot disagree
about it.

---

## Documentation

| Document | |
|---|---|
| [`docs/migrations.md`](./docs/migrations.md) | Migration workflow, and recovering a dirty database |
| [`docs/deployment-topology.md`](./docs/deployment-topology.md) | One server or one per hotel |
| [`docs/multi-tenancy.md`](./docs/multi-tenancy.md) | Serving several hotels from one deployment |
| [`docs/golang-migrate.md`](./docs/golang-migrate.md) | Upstream `migrate` README, kept for reference |

The proposal documents are proposals. They describe work that has **not** been
done, and each says so at the top.

---

## Conventions worth knowing before you change something

These are not stylistic preferences. Each one exists because the alternative
produced a specific bug.

- **Comments argue, and they argue about the rejected options.** A migration
  that adds a nullable column records why it is nullable and which migration
  disagrees. A constraint that could have been stricter records why it is not.
  This is the house style and it is load-bearing: `000029` and `000035` take
  opposite positions on the same question, and the comment is where that is
  resolved.
- **Write the test that fails first.** Several comments here describe an
  assertion that was added *after* a bug was found, and say so. That is the
  point of them.
- **Timezone arithmetic belongs in Go.** `data.Today()` and `localDay()` exist so
  that no read of a date depends on the database session's timezone.
- **Every string goes through `T.en` / `T.pt`.** Parity is enforced: the two
  tables must have the same keys and the same `{token}` placeholders. Colour
  tokens only — `--blue`, `--gray-400`, and nothing spelled as a literal hex in
  a component.
- **Rebuild the UI after every frontend edit**, and check both themes. There is
  a layout-shift trap in any slot that swaps between strings of different
  lengths; reserve the space rather than hoping the copy matches.

---

## Licence

See [`LICENSE`](./LICENSE).
