# Multi-hotel (multi-tenant) support

**Status:** Proposal — not implemented
**Author:** generated during design review
**Scope:** Allow LIRA to serve several hotels, each with its own technicians, catalogs, issues and settings, chosen before login.

---

## 1. Summary

LIRA is currently a single-tenant application: one `application` struct, one `data.Models` value, one connection pool, one database. Supporting several hotels is largely a **routing problem** — deciding which database a request belongs to — not a domain-modelling problem. All the business logic is already correctly scoped by the database, and none of it needs to change.

The core of the work is replacing the single hardcoded pool with a registry of per-hotel pools, and replacing the hardcoded `application.models` with models resolved per request. The second of those is the bulk of the effort and is made safe by removing the field from the struct so the compiler refuses to build until every call site is converted.

**Recommendation:** proceed with a database per hotel, as originally proposed. Section 4 explains why, and what it costs.

---

## 2. Goals

- A visitor picks which hotel they work for **before** authenticating.
- Each hotel is fully independent: its own users, issue types, departments, Connecta agents, consumables, alerts, settings, issues and third-party handovers.
- No operation may read or write another hotel's data. This is a hard security property, not a nice-to-have.
- Adding a hotel is a routine administrative action, not a deployment.
- Schema changes apply to all hotels safely, with visible drift when they are not.
- The existing single-hotel installation becomes hotel #1 with **no data movement**.

## 3. Non-goals

- Cross-hotel reporting or group-level dashboards (see §11.1 — this is the requirement that would invalidate the chosen design).
- Sharing one user identity across hotels. A person working two hotels has two accounts (§12.1).
- Migrating hotels onto different Postgres servers. Supported in the data model, not required now.

---

## 4. Decision: one database per hotel

**Chosen.** Each hotel gets its own database on the shared Postgres instance.

### Why

In multi-tenancy the worst possible defect is one hotel observing another's data. With a shared table and a `hotel_id` column, that defect is a single forgotten `WHERE` clause away, across roughly forty queries, and no amount of review reliably catches it. With a database per hotel, **the leak is structurally impossible** — the tables do not coexist, so there is no query that can return the wrong hotel's rows.

This converts the highest-severity, hardest-to-test risk into an operational cost that is easy to observe and easy to test.

### Alternatives considered and rejected

| Option | Why not |
|---|---|
| One database, `hotel_id` on every table | Cheapest to operate and the only option that makes group-wide reporting trivial, but every query needs a tenant filter and a missed one is a cross-hotel breach. Postgres RLS with a per-transaction `hotel_id` GUC is the standard mitigation and would be worth adding, but it is a second line of defence behind query correctness. |
| One database, one schema per hotel (`hotel_3.issues` via `search_path`) | Middle ground, but `search_path` has to be set per request through a pool using `SET LOCAL` inside a transaction. Subtle, easy to get wrong under concurrency, and awkward for tooling. Isolation is weaker than a separate database for no real benefit at this scale. |
| Sharding one table by hotel | Same class of problem as the `hotel_id` column, plus a routing layer for no gain. |

### What it costs

1. **Connections.** `app_settings`-style singletons and every query are unaffected, but the process now holds N pools. See §7.2 — this is the main operational cost.
2. **Migrations × N.** Every future schema change must be applied to every hotel. See §8.
3. **Cross-hotel aggregation is expensive.** Any "all hotels" query becomes N queries plus a merge. See §11.1.
4. **N backups, N restore drills.** Manageable, but it is now N jobs rather than one.

For the expected scale (a handful of properties) all four are acceptable. If the estate grows to dozens of hotels, §7.2 becomes the dominant cost and should be revisited.

---

## 5. Terminology

**Hotel** is a tenant. Every row of hotel-scoped data belongs to exactly one hotel. The domain word is used throughout, because that is what the operators know the thing as; "tenant" appears only where it clarifies. If hotels ever become something other than hotels (a cruise line, a hospital group), the code should be renamed at that point, not before.

**Directory** is the small shared database holding the list of hotels. It is the only shared state.

---

## 6. Architecture

```
                       ┌──────────────────────────────────────────┐
   browser             │  LIRA process (single binary)            │
  ───────────          │                                          │
   hotel picker  ──────┤  GET /v1/hotels  ──────────┐             │
   login              │                             │             │
   bearer token  ──────┤  authenticate middleware    │             │
   (hotelCode.secret) │   1. split prefix → hotel   │             │
                       │   2. registry.Pool(hotel)  │             │
                       │   3. data.NewModels(pool)  │             │
                       │   4. put user + models in  │             │
                       │      request context       │             │
                       │                             │             │
                       │  handlers: models from ctx │             │
                       └──────────┬──────────────────┘             │
                                  │                               │
                    ┌─────────────┴──────────────┐                │
                    │  Directory DB               │  Hotel DBs     │
                    │  hotels (id, code, name,    │  lira, htb, …  │
                    │        timezone, dbname)    │                │
                    └────────────────────────────┘  full existing │
                                                    schema each    │
```

The hotel database **schema is unchanged**. No table gains a hotel column; no query gains a hotel predicate. Every model in `internal/data` continues to work exactly as it does today, bound to a different `*sql.DB`.

---

## 7. Key design decisions

### 7.1 The directory is the only shared state

The hotel list must be knowable before anyone authenticates, so it cannot live inside any one hotel's database. A small dedicated directory database holds one table:

```sql
CREATE TABLE hotels (
    id            bigserial PRIMARY KEY,
    -- Immutable, permanent, lowercase slug. Used as the auth-token prefix and
    -- in URLs. Renaming a hotel must never change this: tokens issued against
    -- the old slug would stop resolving.
    code          citext UNIQUE NOT NULL CHECK (code ~ '^[a-z0-9][a-z0-9-]{1,31}$'),
    -- Free to change at any time; purely cosmetic.
    name          text NOT NULL,
    -- Hotels differ only by database name, so all of them share the
    -- credentials from -db-dsn. No secret is stored in this table.
    database_name text UNIQUE NOT NULL,
    -- IANA zone. Applied to that hotel's connection pool (§9), not the server.
    timezone      text NOT NULL DEFAULT 'UTC',
    active        boolean NOT NULL DEFAULT true,
    allow_signup  boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT NOW()
);
```

**Connection strings are assembled, not stored.** The process holds one base DSN (`-db-dsn`) and appends `/` + `database_name`. All hotels therefore live on one Postgres instance sharing one credential, and no database password is written to disk. A future hotel that needs a different server can gain a `dsn_override` column later without changing the shape of this design.

**The directory is a single point of failure.** If it is unreachable, nobody can log in anywhere. Mitigation: cache the hotel list in memory and refresh periodically and on change, so an outage degrades rather than blocks. This is required, not optional — see §10.

### 7.2 Per-hotel pools with a global budget

Current configuration (`cmd/api/main.go:51-53`) is `maxOpenConns: 25`, `maxIdleConns: 25`, `maxIdleTime: 15m`, against a PostgreSQL default of `max_connections = 100`. One pool is fine. Four hotels at 25 idle connections exhaust the server.

The relevant number is not `maxOpenConns` — Go opens connections lazily — but `maxIdleConns`, which is what is *retained*:

```
steady-state connections ≈ Σ (maxIdleConns per hotel)
```

Proposed sizing for a small estate, exposed as new flags:

| Setting | Value | Rationale |
|---|---|---|
| `maxIdleConns` per hotel | 5 | Retains a working set without hoarding. 10 hotels ⇒ 50 idle. |
| `maxOpenConns` per hotel | 10 | Burst headroom. Combined with the idle cap, 10 hotels peak at 100. |
| `maxIdleTime` per hotel | 5m | Shorter than today, so a hotel nobody visits releases its pool promptly. |
| Global pool cap | 20 pools | Hard bound; the least-recently-used idle pool is closed beyond it. |

`maxIdleConns` and `maxOpenConns` are today *per-process* settings and become *per-hotel* settings. The existing flags should be reinterpreted as the per-hotel values rather than removed, so existing deployments keep working.

**Eviction must not close a pool mid-request.** Evict only pools with `db.Stats().OpenConnections == 0` that have been unused for longer than the idle window, under the registry's write lock. A pool that is busy, or was used a moment ago, is skipped. This is the one place in the design where a race would produce user-visible "connection refused" errors, so it deserves an explicit test.

### 7.3 The token carries the hotel

The token becomes `<hotelCode>.<secret>`.

This is the highest-leverage decision in the design, because it means `authenticate` can route to the correct database **before performing any lookup at all**:

```
authorization: Bearer htb.KJ3QX7ZR...

  prefix "htb"  →  registry.Hotel("htb")  →  pool  →  data.NewModels(pool)
  secret        →  SHA-256  →  SELECT ... FROM tokens WHERE hash = $1   (in htb only)
```

Consequences worth noting:

- **The `tokens` table needs no new column.** The prefix already identifies the database, so the hotel is not stored in the row.
- **Cross-hotel token replay fails for free.** A token minted in `lira` is simply absent from `htb`'s `tokens` table.
- **The existing scheme is untouched.** Tokens remain opaque base32, SHA-256 hashed, with their existing expiry and scope handling (`internal/data/tokens.go`).
- The hotel **code** must be immutable, which is why §7.1 separates `code` from `name`.

### 7.4 Make the model change compiler-enforced

`application` currently has three fields (`main.go:38-42`), one of which is `models data.Models`. There are **71 `app.models.` call sites across 16 files**.

Delete the field. Then every one of those 71 sites fails to compile, and the compiler keeps a list of exactly what must change. This is the only responsible way to do a refactor this wide: it converts the dangerous failure mode — a handler that silently keeps using the default hotel's data — from something a reviewer must catch into something that does not build.

The conversion is mechanical: handlers read models from the request context, set by the `authenticate` middleware.

### 7.5 Per-hotel timezone via the connection, not the query

`created_at::date` is evaluated by PostgreSQL in the session timezone. The reporting and filtering queries that depend on it are `GetDailyReport`, `GetStats`, `GetRecentDuplicates`, the Analytics model's four range queries, the consumables date filter, and the third-party support range filter. All of them are currently correct only by accident — they inherit the *server's* timezone, which for this installation is `Atlantic/Cape_Verde`.

Setting the timezone on each pool's connections fixes all of them at once, with no change to any SQL:

```
lira_dsn = "postgres://…/lira?sslmode=disable&timezone=Europe/Lisbon"
```

This is a strong argument for per-hotel timezone now rather than later: it is a few lines in pool construction instead of a change to six queries.

One residual gap: the **client** also computes "today" (for example `new Date().toISOString().slice(0,10)` in `addIssue`'s stats refresh and the History page's date picker) and sends it as `?date=`. If a hotel's timezone differs from the browser's, that can be off by a day. The hotel's timezone must therefore be returned to the client at login and used for date defaults.

---

## 8. Provisioning and migrations

### 8.1 Creating a hotel

One operation, manager-only, audited:

1. Generate an immutable `code` from the name, refusing collisions.
2. `CREATE DATABASE <name>` on the shared instance.
3. `CREATE EXTENSION citext` (matching `make db/setup`).
4. Run all migrations to the latest version.
5. Create the first manager account.
6. Insert the `hotels` row.

Step 5 is where the known-default-credentials question in §12.2 has to be decided.

### 8.2 Migrating all hotels

`golang-migrate` is usable as a library, and it already tracks `schema_migrations` **per database** — so per-hotel versions come for free and the existing 17 migrations need no changes at all.

A manager-only admin view should show, per hotel: code, name, current version, and whether it is `dirty`. Actions: migrate one, migrate all.

**Migrating on application startup is not recommended.** With N hotels, one failure would prevent the process from serving the other N−1. Instead: check every hotel's version at startup, log a warning listing any that are behind, and require an explicit manager action to migrate. A hotel that is behind is therefore temporarily un-migratable, which is the correct trade — better than a partially-migrated estate discovered by users.

A `dirty` database must be surfaced prominently and never auto-migrated; it needs a human to resolve.

---

## 9. Request lifecycle

### 9.1 Choosing a hotel (unauthenticated)

```
GET /v1/hotels   →   [{code, name}]  for active hotels only
```

Unauthenticated, so it is reachable from the login screen. It exposes the list of properties, which is normally harmless, but is a conscious decision to record — see §12.3.

### 9.2 Signing in

```
POST /v1/tokens/authentication
  { "hotel": "htb", "email": "...", "password": "..." }
      →  registry.Pool("htb")  →  NewModels(pool).Users.GetByEmail / bcrypt check
      →  NewModels(pool).Tokens.New(userID, 7d, ScopeAuthentication)
      →  "htb.<secret>"
```

`registerUserHandler` takes the same `hotel` field, and is gated on that hotel's `allow_signup`.

`seedDefaultManager` (§12.2) stops running unconditionally at startup and moves into provisioning.

### 9.3 An authenticated request

```
authenticate middleware:
    parse "htb.<secret>"
    hotel := registry.Lookup("htb")            → 404/401 if unknown or inactive
    models := registry.Models(hotel)           → creates or reuses the pool
    user   := models.Users.GetForToken(...)   → in htb's database only
    ctx    := contextSetUser(ctx, user)
    ctx    := contextSetModels(ctx, models)
```

Handlers then use `app.contextGetModels(r)` instead of `app.models`.

### 9.4 Endpoints with no session

These cannot read models from context and need the registry directly. They are the deliberate exception that makes §7.4's compile-time enforcement slightly less than total:

| Endpoint | Why |
|---|---|
| `GET /v1/hotels` | Reads the directory only |
| `GET /v1/healthcheck` | Reports process health, no hotel needed |
| `POST /v1/users` | Resolves the hotel from the request body |
| `POST /v1/tokens/authentication` | Resolves the hotel from the request body |

---

## 10. Failure modes

| Failure | Effect | Mitigation |
|---|---|---|
| Directory DB unreachable | Nobody can log in anywhere | In-memory cache with refresh; degrade to serving cached hotels |
| One hotel's DB down | That hotel's requests fail; others unaffected | Per-hotel error isolation; do not panic the process |
| Pool exhaustion | Requests block until timeout | Global pool cap; per-hotel pool sizing (§7.2); expose pool stats on System Status |
| Migration drift | A hotel's schema is behind | Startup check warns; admin page shows versions per hotel |
| A hotel marked inactive | Existing tokens should stop working | Check `active` in `authenticate`, not just at login |
| Eviction race | Transient "connection refused" | Only evict pools with zero open connections, under lock |

The directory outage is the one genuinely new single point of failure introduced by this design, and it is why §7.1 makes caching a requirement.

---

## 11. Change list

### 11.1 New

| Path | Purpose |
|---|---|
| `migrations/directory/` | Directory schema (`hotels` table) and its own version tracking |
| `cmd/api/hotels.go` | Directory model, registry, pool cache, timezone-aware DSN assembly |
| `cmd/api/hotels_test.go` | Registry behaviour: pool reuse, eviction, timezone |
| `internal/data/migrate.go` | Per-hotel migration runner (golang-migrate as a library) |

### 11.2 Modified

| Path | Change |
|---|---|
| `cmd/api/main.go` | Add `-directory-dsn`; reinterpret pool flags as per-hotel; build the registry instead of one pool |
| `cmd/api/context.go` | Add `modelsContextKey`, `contextSetModels`, `contextGetModels` |
| `cmd/api/middleware.go` | `authenticate` resolves hotel from token prefix and sets models |
| `cmd/api/routes.go` | Add `GET /v1/hotels`; add manager-only hotel and migration routes |
| `cmd/api/seed.go` | `seedDefaultManager` moves into provisioning (§12.2) |
| `cmd/api/tokens.go` | Accept `hotel`; return a prefixed token |
| `cmd/api/users.go` | Accept `hotel`; gate on `allow_signup` |
| `cmd/api/ui/index.html` | Hotel picker, session-per-hotel, hotel name in sidebar |
| `Makefile` | `db/directory-setup`, `db/hotels` targets |

### 11.3 The 71 `app.models.` call sites

Mechanical, but this is where the effort is:

| File | Sites |
|---|---|
| `users.go` | 15 |
| `issues.go` | 14 |
| `alerts.go` | 6 |
| `issue_types.go` | 6 |
| `connecta_agents.go` | 4 |
| `departments.go` | 4 |
| `consumable_items.go` | 4 |
| `tokens.go` | 3 |
| `support_requests.go` | 3 |
| `consumables.go` | 3 |
| `settings.go` | 2 |
| `system.go` | 2 |
| `seed.go` | 2 |
| `analytics.go` | 1 |
| `middleware.go` | 1 |
| `reports.go` | 1 |

The house style is to reach for `app.models` inline. The conversion is to add `models := app.contextGetModels(r)` at the top of each handler and use `models.X` below it — a small, uniform, reviewable change per handler.

### 11.4 Unaffected, worth confirming

- **Every table and migration.** The hotel schema does not change.
- **All business logic** — issues, consumables, third-party support, alerts, analytics, reports. None of it needs to know a hotel exists.
- **Avatars.** `avatar_data` is a `text` column holding a base64 data URL on the `users` row, so there is no shared file storage to partition.
- **`app_settings`.** A single-row singleton per database; each hotel simply gets its own.
- **Consumable stock.** Counts are per-hotel by construction, which is presumably the intent.

---

## 12. Security

### 12.1 Identity is per-hotel

`users.email` is `citext UNIQUE` *within a database*, so the same person working two hotels has two independent accounts with separate passwords, tokens and profile data. Consequences to accept or design around:

- They sign in once per hotel.
- Password resets are per-hotel.
- "Switch hotel" is a frontend convenience: store one session per hotel in `localStorage`, keyed by code, and let the user move between them without re-entering a password.

This is the correct default. Sharing one identity across tenants would create a cross-hotel privilege question that has nothing to do with this feature.

### 12.2 Seeded credentials

`seedDefaultManager` (`cmd/api/seed.go`) creates `lira@lira.local` / `lira` whenever it finds no such account, with a comment noting the password is intentionally short. In a single-hotel deployment this is a convenience. Across many databases it multiplies a known-credential account, and every hotel would have one.

Decide before provisioning ships: either force the first manager to set a password at creation time, or require an explicit `-seed-default-manager` flag so it never happens implicitly.

### 12.3 Unauthenticated endpoints

`GET /v1/hotels` reveals the property list, and `POST /v1/users` allows self-registration. The second is already mitigated by the new `allow_signup` flag, defaulting to `false`. The first should be a conscious decision; if the list is sensitive, the picker can be restricted to hotels that are `active` and publicly listed, with a separate `listed` flag.

### 12.4 Token prefix parsing

Parse the prefix strictly and reject anything unrecognised before touching a pool. An unvalidated prefix is an attacker-controlled string reaching a DSN.

---

## 13. Rollout

The important property: **the code ships dark, and the cutover is a data change.**

| Step | Change | Deployable on its own? |
|---|---|---|
| 1 | Directory schema + registry + pool cache, hardcoded to the existing database | Yes — single-hotel behaviour, directory has one row |
| 2 | Token prefix; login takes a hotel; hotel picker (directory still has one hotel) | Yes |
| 3 | Convert the 71 call sites, enforced by the compiler | Yes |
| 4 | Per-hotel timezone | Yes — set the current zone first, so reports are unchanged |
| 5 | Migration runner + provisioning admin page | Yes |
| 6 | **Add hotel #2 to the directory** | **This is the cutover** |

The existing `lira` database becomes hotel #1 with no data movement: insert one `hotels` row pointing at its database name, and set its timezone to whatever its reports have effectively been using so far.

**Steps 1 and 2 are not independently valuable on their own.** With a picker that can select a hotel whose data the old code does not read, a login for hotel #2 would return hotel #1's data — a cross-tenant breach. The picker is only safe while the directory contains exactly one hotel. The safe increment is therefore steps 1–3 together, with hotel #2 added later.

---

## 14. Testing

The repository currently has **no tests at all**. For this feature that is a gap worth closing deliberately rather than discovering late, because the property that matters is the one that cannot be checked by reading code.

Minimum bar, in priority order:

1. **Cross-hotel isolation.** With two hotels each holding an issue, assert that a user of hotel A cannot read, update or delete hotel B's issue through *any* endpoint, and that a hotel A token presented to hotel B is rejected. This is the test that justifies the whole design.
2. **Registry.** Pool reuse for the same hotel; eviction only of idle pools; timezone applied to the DSN.
3. **Token routing.** Correct hotel selected from the prefix; unknown prefix rejected without opening a pool; a token from one hotel is not found in another.
4. **Date correctness.** Across a month boundary and across hotels in different timezones, confirm "today" resolves in the hotel's zone.

A pragmatic approach for (1) is a test that provisions two scratch databases, applies all migrations to each, and exercises the handlers against both.

---

## 15. Open questions

Answer these before implementation starts; the first two change the design.

1. **Will group-wide reporting across all hotels be needed?** If yes, §4's decision should be revisited now rather than after the refactor. This is the one requirement that meaningfully favours a shared table.
2. **Are the hotels in different timezones?** §7.5 is a few lines either way, but it is far cheaper to set the current zone correctly before there is data in each hotel.
3. **How many hotels to begin with?** Determines the pool budget in §7.2 and whether the global cap needs to be dynamic.
4. **Should the hotel list be public (§12.3)?**
5. **What happens to the seeded default manager (§12.2)?**
6. **Is public self-registration wanted at all**, or should every account be created by a manager? The current endpoint allows anyone to create a technician account.

---

## 16. Effort

Not a configuration change — a refactor with a security property attached. The shape:

| Work | Weight |
|---|---|
| Directory, registry, pool cache, timezone | Contained, self-contained |
| Auth flow, token scoping, login UI | Contained |
| **Converting 71 call sites in 16 files** | **The bulk. Mechanical, wide, and the only part that can introduce a breach** |
| Per-hotel migration runner and provisioning | Contained |
| Isolation tests | Cannot be skipped |

Realistically a multi-day piece of work. The value is concentrated in the third row, and the risk is concentrated there too — which is the argument for doing it with the compiler keeping score rather than by hand.
