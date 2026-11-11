<!-- CODEGRAPH_START -->
## CodeGraph

In repositories indexed by CodeGraph (a `.codegraph/` directory exists at the repo root), reach for it BEFORE grep/find or reading files when you need to understand or locate code:

- **MCP tool** (when available): `codegraph_explore` answers most code questions in one call — the relevant symbols' verbatim source plus the call paths between them, including dynamic-dispatch hops grep can't follow. Name a file or symbol in the query to read its current line-numbered source. If it's listed but deferred, load it by name via tool search.
- **Shell** (always works): `codegraph explore "<symbol names or question>"` prints the same output.

If there is no `.codegraph/` directory, skip CodeGraph entirely — indexing is the user's decision.
<!-- CODEGRAPH_END -->

## Project notes (LIRA)

- Go module: server in `cmd/api`, business logic in `internal/data`, schema in `migrations/`.
- The whole SPA is one file, `cmd/api/ui/src/index.html` (~21k lines). It is indexed: query by symbol or function name (`RoomMapPage`, `IssuesTable`, `AlertsPage`, the `T` translation object) instead of reading the file end to end.
- `cmd/api/ui/index.html` is generated — never edit it directly; rebuild with `node cmd/api/ui/build.mjs`.
- Refresh the graph after a pull or a large edit with `codegraph sync` (it normally auto-syncs). `.codegraph/` is machine-local and git-ignored; the index is not committed.

