# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

The technical (IT/maintenance) department of a Llana hotel: two roles inside one property.

- **Technicians** — the primary users. Phone-first, working across apartments and back-of-house departments, often moving between floors and plant rooms. They log issues in the moment, sometimes with no usable connection, and need the record to survive until it syncs.
- **The manager** — one per hotel. Administers users, issue types, departments, shifts, consumables, alerts and settings; reviews analytics and reports; owns the rota and absence record.

Staff in other departments and guests do not use LIRA; issues reach the IT team through technicians or through Connecta agents (external call handling) and are recorded by the team.

## Product Purpose

LIRA — **Llana IT Reporting & Alerts** — is the maintenance and operations application for a hotel's technical department. It exists so the team stays on top of every technical issue across apartments and departments: logging and resolving issues, preventive maintenance, the team rota and absence, consumables and stock, room surveys, and the daily and attendance reports that come out of all of it.

Success means no technical issue is lost — including one captured on a phone with no connection — every handover to an outside company is evidenced, and the manager can account for the department's work and its people from the same system.

## Positioning

The whole operation runs as one self-contained Go binary with its frontend embedded and a single PostgreSQL database: no CDN, no build step at run time, no external services. The team can keep working on the hotel's own network, and a phone can capture an issue offline and send it when the connection returns. Full English/European-Portuguese parity is built in rather than bolted on.

That combination — self-hosted simplicity, offline-first capture, and a genuinely bilingual operational record — is the durable difference; a neighbouring cloud-only tool could not truthfully claim it.

## Operating Context

- A single hotel property, with issues split between **apartments (`apt`)** and **departments (`dept`)**.
- The hotel runs named shifts (for example Morning, Late, Night); the rota and absence record are part of daily operations.
- The department hands work to outside companies — **Telnet** and **Telefonica** — and records who was told, when, and who confirmed the fix.
- **Connecta agents** answer calls and report issues on the team's behalf; the reporter is part of the issue record.
- Room-level technical estate includes TVs and **LCUs** (room control units), with a room map used for surveys and TV swaps between rooms.
- Technicians use phones; the manager also works from a desktop. Both themes (light/dark) are used.
- Daily and attendance reports are produced as PDF and XLSX for department and hotel use.
- The application never migrates the database on startup; migrations are a deliberate, separate operation.

## Capabilities and Constraints

Confirmed functionality:

- Issue logging, editing, assignment, history, photos/attachments, start/resolve timestamps, false positives, and offline capture with later sync.
- Preventive maintenance with assignment and linked issues.
- Recurring-alert detection (the same issue type recurring at the same location within a window), with an alert inbox.
- Team rota: shifts, week cells, absences, rota members, positions.
- Consumables catalogue, usage on issues, and stock levels with low-stock surfacing.
- Room map and room-level equipment; LCU testing and reconditioned-unit records; TV swaps.
- Third-party handover (Telnet/Telefonica) and support-request records with durations.
- Analytics and reports: daily and attendance renderers producing PDF and XLSX.
- Users, roles (`technician`, `manager`), profile, password reset, per-device tokens so a phone and a desk can stay signed in at the same time.
- Notification read state and an in-app inbox.
- English and European Portuguese, with enforced key/placeholder parity.

Technical constraints:

- Go 1.22, `net/http` + httprouter, PostgreSQL with the `citext` extension, `lib/pq`.
- Frontend is React authored as JSX in one file and compiled with a pinned esbuild; the compiled HTML is committed and embedded with `go:embed`. The UI must be rebuilt (`make ui`) after every frontend edit.
- Bearer-token auth; no external runtime dependencies or services.
- Timezone arithmetic for dates belongs in Go (`data.Today()`, `localDay()`), not in database session settings.
- Colour tokens only in components (no literal hex); both themes must be checked; parity of translation keys and `{token}` placeholders is enforced.
- Business rules that decide what a request means live in `internal/data`, not in handlers.

Undecided product facts (do not assume them as shipped):

- Multi-hotel / multi-tenant operation is **proposed, not implemented**. The current application is single-tenant. The deployment topology (one central server versus one server per hotel) is likewise an open proposal.
- There is no pricing, licensing model, customer list, or benchmark evidence in the repository.

## Brand Commitments

- Name: **LIRA**, expanded as **Llana IT Reporting & Alerts**. The Llana identity is binding; this is not a white-label product.
- Voice: direct, operational, plain-spoken; English and European Portuguese (pt-PT) are equal citizens and must stay in parity.
- Existing app icons and manifest are part of the identity.
- Language is deliberate: terminology distinguishes apartments from departments and a rota from a booking; do not casualise operational terms.

## Evidence on Hand

- Working single-hotel application in this repository: handlers in `cmd/api`, rules in `internal/data`, report renderers in `internal/report`, schema in `migrations/`, frontend source in `cmd/api/ui/src/index.html`.
- Demo users in `migrations/seed.sql`: Manager (manager), Eder and Alcidio (technicians). These are placeholders, not real staff profiles.
- Real operational vocabulary and handover companies come from the code and copy (Telnet, Telefonica, Connecta).
- No real testimonials, case studies, customers, press, pricing, deployment claims, or performance benchmarks exist. Future work must not fabricate them.
- Proposal documents in `docs/` describe work that has **not** been done and say so at the top.

## Product Principles

1. **Never lose the record.** A capture taken offline or in a hurry is still a real issue; durability outranks immediacy of display.
2. **One deployable, no dependencies.** Prefer solving inside the binary and the database over adding a service, CDN, or runtime.
3. **The department's truth, not a dashboard's approximation.** Handover evidence, timestamps, and stock counts must reflect what happened.
4. **Bilingual is structural.** Every string, placeholder, and operational term exists in both languages at once; parity is a release condition.
5. **The rule lives where the meaning lives.** Business meaning belongs in the data layer so two entry points cannot disagree.

## Accessibility & Inclusion

Target **WCAG 2.1 AA**: keyboard-operable controls, visible focus, readable contrast in both light and dark themes, and touch targets usable one-handed on a phone in the field. Do not rely on colour alone to carry status; the existing colour-token discipline exists partly for this reason.
