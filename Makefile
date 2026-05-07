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
.PHONY: db/migrations/up
db/migrations/up: confirm
	@echo 'Running up migrations...'
	migrate -path ./migrations -database ${LIRADB_DSN} up

## db/migrations/down: apply all down database migrations
.PHONY: db/migrations/down
db/migrations/down: confirm
	@echo 'Running down migrations...'
	migrate -path ./migrations -database ${LIRADB_DSN} down

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
