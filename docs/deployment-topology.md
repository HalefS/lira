# Deployment topology: one server for all hotels, or a server per hotel?

**Status:** Proposal — not implemented
**Companion document:** [`multi-tenancy.md`](./multi-tenancy.md)
**Scope:** Where does the LIRA application and its database physically run?

---

## 1. The question, and why it is not just a hosting question

Two topologies are on the table:

- **A — Central.** One server, one LIRA instance, one PostgreSQL instance, all hotels served by it. The hotels are already on a common network.
- **B — Distributed.** A server in each hotel, running LIRA and PostgreSQL locally, one per property.

The important thing to recognise is that **this is not purely an infrastructure decision — it determines whether the multi-tenancy work happens at all.**

| | Central (A) | Distributed (B) |
|---|---|---|
| One LIRA serving several hotels? | Yes | No — each serves exactly one |
| Need the multi-tenancy refactor? | **Yes**, per `multi-tenancy.md` | **No** |
| Directory database, pool registry, token prefix, 71 call-site refactor? | All required | None |
| Hotel picker before login? | Yes | No — you reach the right server by URL |
| Cross-hotel data leakage possible? | Only if implemented wrong | Structurally impossible |

Topology B deletes an entire feature's worth of engineering *and* an entire feature's worth of security risk. The app stays exactly as it is. That is the single most consequential fact in this document, and it is worth settling before spending effort on either.

The converse is equally true: if the long-term intention is to **sell LIRA as a hosted product to hotels you do not own**, then A is the only workable shape. A self-hosted appliance (B) means shipping and remotely supporting hardware at customer sites you do not control.

So the topology question reduces almost entirely to one question — **is this a chain you operate, or a product you sell?** — plus how much you value local autonomy.

---

## 2. Option A — Central: one server, one PostgreSQL, all hotels

One server hosts LIRA and a single PostgreSQL instance holding one database per hotel (the arrangement in `multi-tenancy.md`).

### Pros

- **One version to build, deploy and patch.** No version drift across sites, because there is only one site. A bug fix ships once.
- **One place to monitor.** The existing System Status page covers the whole estate; no external uptime monitoring is needed per property.
- **Cheaper, in hardware and in effort.** One server instead of *N*, and one operating system and PostgreSQL to keep patched rather than *N*.
- **Backups are one automated job.** Per logical database, but one system, one tool, one place to verify the backups actually restored.
- **Adding a hotel is a data change,** not a hardware order. Minutes instead of weeks.
- **No connection-budget pressure.** One pool, one database server.
- **Central administration.** Config changes, catalogue fixes and support work all happen once.
- **Cross-hotel visibility is possible**, and is the natural shape if group reporting is ever wanted.
- **It is the standard SaaS shape,** so the work in `multi-tenancy.md` is reusable rather than thrown away if the product is later commercialised.

### Cons

- **It requires the full multi-tenancy refactor.** The 71 call sites, a directory database, a per-hotel pool registry, token routing, a hotel picker. Real work, and the one part of it — the call-site conversion — is where a cross-hotel data leak could be introduced.
- **A new single point of failure.** The directory database in that design becomes the thing every login depends on. If it is unavailable, nobody signs in anywhere.
- **A second, larger single point of failure.** The server itself. With *N* hotels on one machine, its failure takes down all of them. Central topology needs real HA — replication, failover, tested restore — to match what B gives you for free.
- **Blast radius of mistakes.** All hotels' data in one PostgreSQL instance. A mistyped `DELETE`, a bad migration, or a destructive script that escapes its filter affects every property at once. Separate databases limit the damage to one hotel, but the *instance* is still shared, so an instance-level mistake is not contained.
- **Every interaction crosses the network.** Each page load and every API call is a round trip to the central server. Reachability is not the issue — you have said the network is fine — but **latency and availability are separate questions** and both are worse:
  - A technician on a phone in a hotel basement loading the issue list feels every millisecond of the WAN round trip. On a poor link the app feels sluggish in a way it never would locally.
  - If the link drops, **that hotel stops logging issues entirely.** There is no local fallback.
- **Avatars are base64 in the database** (`users.avatar_data` is a `text` column). Every user-list response carries image bytes over that link. Small at your scale, but it is payload on a WAN round trip that could be served from a local disk instead.
- **Central target, central blast radius.** A breach of the central server exposes every hotel's data at once.
- **Hosting is a business commitment.** Once you run this for others, uptime, backups and incident response become obligations rather than internal IT chores.
- **Noisy neighbour is possible**, mitigated but not eliminated by separate databases and separate connection pools.

---

## 3. Option B — Distributed: a server in each hotel

One server per property, each running LIRA and its own PostgreSQL. The current application, unmodified.

### Pros

- **No multi-tenancy work whatsoever.** The application is unchanged. `multi-tenancy.md` is not needed. This is the headline benefit and it is not small: it removes both a substantial refactor and its associated risk.
- **No cross-hotel risk of any kind.** The data never coexists, so no query, bug or migration can expose one hotel to another.
- **Fast.** Local round trips, including for avatars. A phone on hotel Wi-Fi performs the same as a desktop.
- **The hotel keeps working if the WAN drops.** For a field-service tool used inside the building, local autonomy is a genuine operational property, not a nicety.
- **Failure is isolated per site.** One hotel's server dies; the other hotels are untouched. No shared blast radius, and no need for failover engineering.
- **Data stays in the building.** May matter for contractual or data-residency reasons, depending on the hotels and the data involved.
- **No connection-budget problem.** One pool, one database, the current 25-connection ceiling is fine.
- **Genuinely sellable as a product.** Shipping an appliance to an unrelated customer avoids making hosting an ongoing service commitment.
- **Backups can be tuned per hotel** to match how much that property's data matters.

### Cons

- **N servers to buy, rack, power, cool, connect and monitor.** Hardware cost and physical footprint scale linearly with the estate.
- **Version drift is the serious operational hazard.** *N* deployments means *N* opportunities for sites to diverge. Once sites run different versions, bug reports cannot be reproduced reliably and support becomes untenable. This needs a strict policy — every site on the same version — plus a way to verify it, not merely assume it.
- **Schema migration becomes a fleet operation.** You roll a migration out site by site. During the rollout, some sites are on the old schema and some on the new, so **the application must be compatible with both** or the rollout has to be all-at-once per site with an accepted outage. A half-migrated hotel is broken, and there is no single transaction to roll back across the estate.
- **Nobody is watching at 2am.** A server in a hotel closet is not monitored by default. Without external alerting, a failure is discovered the next morning. This needs uptime monitoring per site, which partly reintroduces a network dependency.
- **Hardware failure at a site is an outage until someone goes there.** Hotels rarely have technical staff on site, so this needs a spares strategy and a named local contact per property.
- **Backups are local, and that is a durability risk.** Fire, flood or theft at a property means total loss unless backups go offsite — and offsite backup depends on the WAN link, so a link failure at 3am means no backup either. B's availability advantage and its backup exposure are the same WAN link, pulling opposite ways.
- **No central view.** Any group-wide reporting becomes a genuine project: each site pushes data somewhere, or a central read-only database is fed from each. This is the requirement that hurts B most severely, and §6 returns to it.
- **Security patching is repeated *N* times** — operating system, PostgreSQL, and the Go binary.
- **Support requires remote access to each site,** which is both an operational burden and a security question when the sites belong to customers.
- **Onboarding a new hotel is slow and manual:** order hardware, ship, install, network, seed, migrate.
- **One shared weakness is hidden per-site.** Every site runs the same version, so a vulnerability in the app affects all of them — the same blast radius as A for *software* problems, even though it is better for *hardware* and *network* problems. B trades one blast radius for another; it does not eliminate it.

---

## 4. Side by side

| | A — Central | B — Distributed |
|---|---|---|
| Application changes | Full multi-tenancy refactor | **None** |
| Servers to operate | 1 | N |
| Versions to keep in sync | 1 | N |
| Response time | WAN round trip | Local |
| Works if WAN fails | No | Yes |
| Failure blast radius | All hotels | One hotel |
| Hardware cost | 1× | N× |
| Adding a hotel | Minutes | Weeks |
| Backups | One system, automated | N local, needs offsite copy |
| Group-wide reporting | Feasible | Significant extra work |
| Cross-hotel data leak risk | Real, must be engineered away | Impossible |
| Commercialising as a product | Natural (hosted SaaS) | Possible, heavier support |
| Isolation from hardware failure | Needs failover engineering | Automatic |

---

## 5. The factors that should decide it

In rough order of how much they should move the decision:

1. **Do you operate these hotels, or would you sell the software?** A chain you control can justify B. A product you sell needs A — you cannot ask a customer to host a server.
2. **Do hotels need to keep working through a network outage?** If yes, B. This is the strongest single argument for it, and it is about the field technicians' experience rather than the server room.
3. **How much autonomy does each hotel have over its own IT?** Where the hotel has its own IT staff, B is natural. Where it does not, B puts hardware in a room nobody visits.
4. **How many hotels?** Under about five, the extra hardware and site visits of B outweigh its benefits. Beyond about fifteen, the version-drift and migration-rollout burden of B becomes the dominant operational cost, and a central deployment with HA starts to look better.
5. **Will you need group-wide reporting?** If yes, A is much cheaper. If it is a firm requirement, settle it now — it is the requirement most likely to reverse a B decision later.
6. **Are hotels in different countries with mediocre links?** Reachability is not in question, but latency and link uptime still are, and they weigh against A.
7. **Is there technical staff on site at each hotel?** If not, B's unattended-hardware problem is real.

---

## 6. If you need both: the honest hybrid

If local autonomy and group reporting are both genuinely required, the answer used at scale is neither pure option:

> **LIRA deployed per site (B), plus a central analytics store that each site pushes a summary or event stream to.**

Each hotel keeps its own database and keeps working offline. A central read-only database accumulates what the group wants to see. The per-site application stays unmodified, so none of the multi-tenancy complexity is needed.

The cost is real and should not be waved away:

- A replication or ETL mechanism to design, secure and operate.
- A second data model for the central store, and the question of what happens when a site is offline for a week and syncs late.
- The central store becomes a privileged target holding every hotel's data, reintroducing a concentration risk.
- Duplicated security concerns and another thing to back up.

This is the right answer if you have the appetite for a second project. It is not a free way to have both, and it is more work than picking A and accepting the WAN dependency.

**Not recommended:** splitting the estate between topologies (some hotels central, some local). It doubles the code paths, the deployment procedures and the test matrix, to avoid making one decision.

---

## 7. Recommendation

Conditional, because the decisive inputs are business facts rather than technical ones:

**Choose A (central) if:**
- the long-term plan is a hosted product, or the hotels are numerous enough that *N* site visits and *N* deployments become a burden; or
- group-wide reporting is a requirement you can name today; or
- the estate is small (roughly under five) and every hotel has reliable connectivity.

In that case `multi-tenancy.md` is the prerequisite, and its rollout plan is the implementation.

**Choose B (distributed) if:**
- these are hotels you operate, the count is modest, and you can commit to keeping every site on the same version; and
- technicians genuinely need the app to keep working during a network outage, or the links make a central round trip feel slow; and
- group-wide reporting is not a near-term requirement.

In that case **disregard `multi-tenancy.md` entirely.** The application is already correct for this topology, and the work is hardware, a rollout procedure, per-site monitoring, and a documented answer to version drift and migration ordering.

**If you are unsure:** A is the more common end state and does not foreclose B later. B, once deployed, is expensive to reverse and much harder to withdraw from sites. That asymmetry favours starting central if group reporting might matter, and starting distributed only if local autonomy is a firm requirement today.

---

## 8. Before deciding

Answering these collapses most of the uncertainty:

1. **Chain you operate, or product you sell?** The one question that settles the shape.
2. **Roughly how many hotels, and over what horizon?**
3. **Must a hotel keep working during a WAN outage?**
4. **Will anyone need a view across all hotels? When?**
5. **Are hotels in different countries, and how good are the links in practice — not just up/down, but latency?**
6. **Is there anyone on site at a hotel who could restart a server?**
7. **Who applies a release: you, centrally, or each hotel's own IT?**
8. **Are there contractual or data-residency requirements keeping a hotel's data in its building?**

Questions 1, 3 and 4 are decisive. The rest refine the operational plan.
