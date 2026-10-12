include .envrc

# ==================================================================================== #
# HELPERS
# ==================================================================================== #

.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

.PHONY: confirm
confirm:
	@echo -n 'Are you sure? [y/N] ' && read ans && [ $${ans:-N} = y ]

# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #
#
# PDF reports are printed by a headless Chromium (Chrome, Chromium or Edge). The
# binary is auto-detected on startup; override it with -report-chrome-bin=... or
# the LIRA_CHROME_BIN environment variable if it is installed somewhere unusual.
# Without one, the server still starts and only the PDF endpoint is disabled.

## run/api: run the cmd/api application
.PHONY: run/api
run/api:
	go build -trimpath -o ./bin/lira ./cmd/api && ./bin/lira -db-dsn=${LIRADB_DSN}

## run/api/dev: run with extra dev flags (no rate limit, verbose)
.PHONY: run/api/dev
run/api/dev:
	go build -trimpath -o ./bin/lira ./cmd/api && ./bin/lira \
		-db-dsn=${LIRADB_DSN} \
		-env=development \
		-limiter-enabled=false

# ==================================================================================== #
# DATABASE
# ==================================================================================== #

## db/psql: connect to the database using psql
.PHONY: db/psql
db/psql:
	psql ${LIRADB_DSN}

## db/migrations/up: apply all up database migrations
#
# `tr -d '\r'` in the recipes below is not decoration. psql on Windows terminates its
# output with CRLF, and `$(...)` strips the newline but NOT the carriage return -- so
# the value read back is "t\r", `[ "$d" = "t" ]` is false, and a dirty database reads
# as clean. That failure is silent and worse than having no guard at all: it looks
# like the check is passing.
#
# So: apply migrations with `migrate`. Never with `psql`. See docs/migrations.md.
#
# The dirty check below is the important part, and it is here because the failure it
# prevents is confusing rather than obvious.
#
# `psql -f migrations/0000NN.up.sql` applies a migration PERFECTLY -- every table,
# column and trigger is created -- and records nothing, because schema_migrations is
# written only by the migrate CLI. The next `up` then reads an old version, believes
# it, tries to re-apply a migration whose objects already exist, fails, and marks the
# database DIRTY. From that point every migrate command refuses, and the error names
# the migration that FAILED, which is usually not the one you were working on.
#
# So the guard prints what dirty means and how to get out of it, rather than letting
# migrate print "Fix and force version" and leaving the reader to work out what that
# means. See docs/migrations.md.
.PHONY: db/migrations/up
db/migrations/up: confirm
	@d=$$(psql ${LIRADB_DSN} -At -c 'SELECT dirty FROM schema_migrations' 2>/dev/null | tr -d '\r'); \
	if [ "$$d" = "t" ]; then \
		v=$$(psql ${LIRADB_DSN} -At -c 'SELECT version FROM schema_migrations' 2>/dev/null | tr -d '\r'); \
		echo ""; \
		echo "  This database is DIRTY at version $$v."; \
		echo "  The schema and schema_migrations disagree, and migrate will refuse to"; \
		echo "  run until the version is forced."; \
		echo ""; \
		echo "  CHECK THE SCHEMA FIRST -- force runs no SQL and verifies nothing:"; \
		echo "    make db/migrations/status"; \
		echo "  then, if every object is really there:"; \
		echo "    migrate -path ./migrations -database ${LIRADB_DSN} force $$v"; \
		echo ""; \
		echo "  Full procedure: docs/migrations.md"; \
		echo ""; \
		exit 1; \
	fi
	@echo 'Running up migrations...'
	migrate -path ./migrations -database ${LIRADB_DSN} up

## db/migrations/down: apply all down database migrations
.PHONY: db/migrations/down
db/migrations/down: confirm
	@echo 'Running down migrations...'
	migrate -path ./migrations -database ${LIRADB_DSN} down

## db/migrations/status: report the applied version, and flag a dirty database
#
# Read-only. It never forces anything, because `force` runs no SQL and so verifies
# nothing -- a status command that could silently paper over a half-applied migration
# would be the wrong place to put that power.
.PHONY: db/migrations/status
db/migrations/status:
	@v=$$(psql ${LIRADB_DSN} -At -c 'SELECT version FROM schema_migrations' 2>/dev/null | tr -d '\r'); \
	d=$$(psql ${LIRADB_DSN} -At -c 'SELECT dirty FROM schema_migrations' 2>/dev/null | tr -d '\r'); \
	if [ -z "$$v" ]; then \
		echo "  No schema_migrations table. This database has never been migrated."; \
		echo "  Run: make db/migrations/up"; \
	elif [ "$$d" = "t" ]; then \
		echo "  DIRTY at version $$v"; \
		echo "  A migration failed partway. migrate will refuse every command until"; \
		echo "  the version is forced, and forcing runs no SQL."; \
		echo "  Check the schema first, then see docs/migrations.md section 5."; \
	else \
		echo "  Clean, version $$v"; \
		echo "  migrate: $$(migrate -path ./migrations -database ${LIRADB_DSN} version 2>&1 | tr -d '\r')"; \
	fi

## db/migrations/doctor: report what the database actually contains, read-only
#
# The question `status` cannot answer on its own: is the schema AHEAD of
# schema_migrations? That is the state behind every "already exists" error, and it
# is invisible from the version alone -- the version looks perfectly normal.
#
# Pure SELECTs. Safe on a live database with the server running, and safe on a
# production one, because it writes nothing. See docs/migrations.md section 4.
.PHONY: db/migrations/doctor
db/migrations/doctor:
	@psql ${LIRADB_DSN} -v ON_ERROR_STOP=1 -f ./scripts/migrate-doctor.sql

## db/migrations/dryrun v=35: run one up migration inside a transaction, then roll back
#
# FOR REPRODUCING A FAILING MIGRATION. `migrate` does not log why a migration failed
# -- it prints the error once and then only says "Dirty database version N" -- so on
# a database that is already dirty there is nothing left to read.
#
# This runs the migration file for real, inside a transaction that is rolled back, so
# the exact failing statement and its exact error are printed and NOTHING persists.
# Postgres DDL is transactional, which is what makes this safe on production: a
# CREATE TABLE that succeeds here is undone before psql exits.
#
# ON_ERROR_STOP is deliberately 0. With it set, psql aborts on the first error and
# never reaches the ROLLBACK -- leaving the transaction open, to be discarded anyway
# when the connection closes, but only after printing nothing useful about what came
# after the failure. The rollback must be unconditional.
#
# Not a substitute for reading the migration. It tells you WHICH statement failed,
# not what the author meant.
.PHONY: db/migrations/dryrun
db/migrations/dryrun:
	@p=$$(printf '%06d' $(v) 2>/dev/null); \
	f=$$(ls ./migrations/$${p}_*.up.sql 2>/dev/null | head -1); \
	if [ -z "$$f" ]; then \
		echo "  No migration file for version '$(v)' (looked for migrations/$${p}_*.up.sql)."; \
		echo "  Usage: make db/migrations/dryrun v=35"; \
		exit 1; \
	fi; \
	echo "  Dry run of $$f -- every statement below is rolled back."; \
	echo ''; \
	printf 'BEGIN;\n\\i %s\nROLLBACK;\n' "$$f" | psql ${LIRADB_DSN} -v ON_ERROR_STOP=0

## db/migrations/lint: parse every migration file and report the ones that are not valid SQL
#
# Exists because of a bug that shipped in 5417b03 and could not reach production
# any other way. One line of a comment in 000035 lost its leading `--`, so the file
# stopped being a comment and became a bare sentence sitting in a CREATE TABLE
# script. It was a SYNTAX ERROR, and therefore fatal -- but the migration had
# already been "applied" locally, so nobody ran it again, and the only evidence it
# was broken was a deploy on another server failing with an error that named line 158
# of a file the author had written that morning.
#
# Every file is run through the real parser inside a transaction that is rolled
# back. Only PARSE errors are reported -- `syntax error`, `unterminated quoted
# string`, `incomplete input`. Those are state-independent: a file with one is
# invalid against an empty database, a full database and a production one alike, so
# this lints correctly on a database where everything is already applied and every
# statement is a harmless "already exists" notice.
#
# State-dependent failures ("already exists", duplicate key) are DELIBERATELY not
# reported here. On a migrated database every such statement is expected, and
# reporting them would bury the one line that matters.
#
# Safe against a live database: DDL is transactional, so every statement is undone
# before psql exits. The migrations contain no CONCURRENTLY, VACUUM, ALTER TYPE,
# CREATE DATABASE or CREATE EXTENSION, all of which would refuse to run inside a
# transaction and are the reason this is safe to point at production.
.PHONY: db/migrations/lint
db/migrations/lint:
	@fail=0; n=0; \
	for f in ./migrations/0*.sql; do \
		n=$$((n+1)); \
		out=$$(printf 'BEGIN;\n\\i %s\nROLLBACK;\n' "$$f" | psql ${LIRADB_DSN} -v ON_ERROR_STOP=0 -q 2>&1); \
		if echo "$$out" | grep -qE 'syntax error|unterminated quoted string|incomplete input'; then \
			echo "  PARSE ERROR  $$f"; \
			echo "$$out" | grep -E 'ERROR|LINE [0-9]' | sed 's/^/      /'; \
			fail=1; \
		fi; \
	done; \
	if [ $$fail -eq 0 ]; then \
		echo "  $$n migration files parse"; \
	else \
		echo ''; \
		echo "  A migration file is not valid SQL. Nothing above this line is a bug --"; \
		echo "  every other statement ran and was rolled back."; \
		exit 1; \
	fi

## ui/check-names: every person name printed outside the rota is shortened
#
# 48 render sites were found by hand and edited by line number, and a MISS is
# invisible -- the page still renders, the name is just still too long. There are no
# JS tests in this repository and no lint step, so without this nothing would
# notice. It found three real misses the first time it ran.
#
# A source scan, not a browser check: it proves every name that is PRINTED is
# wrapped, and it cannot notice a person-name field nobody has added to its list.
.PHONY: ui/check-names
ui/check-names:
	@python scripts/check-short-names.py

## db/migrations/new name=$1: create a new database migration
.PHONY: db/migrations/new
db/migrations/new:
	@echo 'Creating migration files for ${name}...'
	migrate create -seq -ext=.sql -dir=./migrations ${name}

## db/setup: create the database and enable citext extension
.PHONY: db/setup
db/setup:
	psql -c "CREATE DATABASE lira;"
	psql -d lira -c "CREATE EXTENSION IF NOT EXISTS citext;"

## db/seed: insert demo users for development
.PHONY: db/seed
db/seed:
	@echo 'Seeding database...'
	psql ${LIRADB_DSN} -f ./migrations/seed.sql

# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## audit: tidy deps and format, vet, staticcheck, test
.PHONY: audit
audit:
	@echo 'Tidying and verifying module dependencies...'
	go mod tidy
	go mod verify
	@echo 'Formatting code...'
	go fmt ./...
	@echo 'Vetting code...'
	go vet ./...
	@echo 'Running tests...'
	go test -race -vet=off ./...

# ==================================================================================== #
# BUILD
# ==================================================================================== #

## ui: compile the JSX in cmd/api/ui/src/index.html into the embeddable cmd/api/ui/index.html
#
# The app shell is authored as JSX and compiled to JavaScript here, once, rather
# than in the browser on every page load. It used to be compiled by Babel
# standalone fetched from a CDN, which cost ~3.7s to download and then ~6.6s of
# main-thread transpile on every single page load -- measured, not estimated.
#
# cmd/api/ui/index.html is generated and committed, so `go build` and every build
# target above still need nothing but Go. Node is needed only to regenerate it,
# which is what this target is for: edit cmd/api/ui/src/index.html, then run this.
#
# Requires node and npx on PATH. esbuild is pinned in build.mjs so the output does
# not depend on which version happens to be current.
.PHONY: ui
ui:
	@echo 'Compiling the frontend...'
	node cmd/api/ui/build.mjs

## build/api: build for current OS (single binary with embedded frontend)
.PHONY: build/api
build/api:
	@echo 'Building ./bin/lira...'
	go build -trimpath -ldflags='-s' -o=./bin/lira ./cmd/api

## build/linux: build for Linux amd64 (e.g. a VPS or server)
.PHONY: build/linux
build/linux:
	@echo 'Building ./bin/linux_amd64/lira...'
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s' -o=./bin/linux_amd64/lira ./cmd/api

## build/windows: build for Windows 11 x86-64
.PHONY: build/windows
build/windows:
	@echo 'Building ./bin/windows_amd64/lira.exe...'
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s' -o=./bin/windows_amd64/lira.exe ./cmd/api

## build/mac/intel: build for macOS Intel (x86-64)
.PHONY: build/mac/intel
build/mac/intel:
	@echo 'Building ./bin/darwin_amd64/lira...'
	GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags='-s' -o=./bin/darwin_amd64/lira ./cmd/api

## build/mac/arm: build for macOS Apple Silicon (M1/M2/M3)
.PHONY: build/mac/arm
build/mac/arm:
	@echo 'Building ./bin/darwin_arm64/lira...'
	GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags='-s' -o=./bin/darwin_arm64/lira ./cmd/api

## build/all: build for all platforms at once
.PHONY: build/all
build/all: build/api build/linux build/windows build/mac/intel build/mac/arm
	@echo 'All binaries built:'
	@ls -lh ./bin/lira ./bin/linux_amd64/lira ./bin/windows_amd64/lira.exe ./bin/darwin_amd64/lira ./bin/darwin_arm64/lira
