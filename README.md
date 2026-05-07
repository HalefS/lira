# LIRA — Llana IT Reporting & Alerts

Go + PostgreSQL REST API for LIRA, the IT issue tracker for Hotel Llana. Structured after Alex Edwards' *Let's Go Further* greenlight pattern.

---

## Project Structure

```
lira-backend/
├── cmd/
│   └── api/
│       ├── main.go          # Entry point, DB pool, server startup
│       ├── routes.go        # All route definitions
│       ├── frontend.go      # Serves embedded frontend at /
│       ├── middleware.go    # Auth, CORS, rate limiting, panic recovery
│       ├── context.go       # Request context helpers
│       ├── helpers.go       # JSON read/write, param parsing
│       ├── errors.go        # Standardised error responses
│       ├── users.go         # POST /v1/users, GET /v1/users
│       ├── tokens.go        # POST /v1/tokens/authentication
│       ├── issues.go        # Full CRUD /v1/issues + /v1/stats
├── internal/
│   ├── data/
│   │   ├── models.go        # Models struct + sentinel errors
│   │   ├── users.go         # UserModel (insert, get, getByEmail, getForToken)
│   │   ├── tokens.go        # TokenModel (generate, insert, delete)
│   │   └── issues.go        # IssueModel (CRUD, filters, stats)
│   └── validator/
│       └── validator.go     # Input validation helpers
├── ui/
│   ├── ui.go                # //go:embed — bundles web/ into the binary
│   └── web/
│       └── index.html       # The LIRA frontend (served at /)
├── migrations/
│   ├── 000001_create_users_table.up.sql
│   ├── 000002_create_tokens_table.up.sql
│   ├── 000003_create_issues_table.up.sql
│   └── seed.sql
├── .envrc                   # DSN env var (git-ignored)
├── .gitignore
├── Makefile
├── go.mod
└── go.sum
```

---

## Prerequisites

| Tool | Purpose | Install |
|------|---------|---------|
| Go 1.22+ | Build & run | https://go.dev/dl/ |
| PostgreSQL 14+ | Database | https://www.postgresql.org/download/ |
| `migrate` CLI | Run migrations | `brew install golang-migrate` or see below |

### Install migrate CLI
```bash
# macOS
brew install golang-migrate

# Linux
curl -L https://github.com/golang-migrate/migrate/releases/download/v4.17.0/migrate.linux-amd64.tar.gz | tar xvz
sudo mv migrate /usr/local/bin/

# Windows (scoop)
scoop install migrate
```

---

## Setup

### 1. Create the PostgreSQL database

```bash
# Connect as superuser
psql -U postgres

# Inside psql:
CREATE USER lira WITH PASSWORD 'your_secure_password';
CREATE DATABASE lira OWNER lira;
\c lira
CREATE EXTENSION IF NOT EXISTS citext;
\q
```

### 2. Configure the DSN

Edit `.envrc`:
```bash
LIRADB_DSN=postgres://lira:your_secure_password@localhost/lira?sslmode=disable
```

If you use `direnv`:
```bash
direnv allow
```

Otherwise, export manually:
```bash
export LIRADB_DSN=postgres://lira:your_secure_password@localhost/lira?sslmode=disable
```

### 3. Run migrations

```bash
make db/migrations/up
```

### 4. Install Go dependencies

```bash
go mod tidy
```

### 5. Start the API

```bash
make run/api
```

For development (no rate limiting, verbose):
```bash
make run/api/dev
```

Server starts on **http://localhost:4000**

---

## API Reference

All protected endpoints require:
```
Authorization: Bearer <token>
```

### Auth

#### Register
```
POST /v1/users
Content-Type: application/json

{
  "name": "Eder Silva",
  "email": "eder@hotel.com",
  "password": "secret123",
  "role": "technician"        // "technician" | "manager"
}
```
Returns `user` + `token` (auto-login on register).

#### Login
```
POST /v1/tokens/authentication
Content-Type: application/json

{
  "email": "eder@hotel.com",
  "password": "secret123"
}
```
Returns `user` + `token`.

---

### Users

#### List all users
```
GET /v1/users
Authorization: Bearer <token>
```

#### Get single user
```
GET /v1/users/:id
Authorization: Bearer <token>
```

---

### Issues

#### List issues
```
GET /v1/issues
Authorization: Bearer <token>

Query params:
  mode=apt|dept
  status=Ok|Pending
  type=Door|Internet|Hardware|...
  search=<text>
  date=2026-03-21          (YYYY-MM-DD, defaults to all)
```

#### Create issue
```
POST /v1/issues
Authorization: Bearer <token>
Content-Type: application/json

{
  "mode": "apt",            // "apt" | "dept"
  "location": "1369",       // apartment number or department name
  "type": "Door",
  "problem": "Door blocked",
  "resolution": "Battery replaced, reprogrammed",
  "time_minutes": 8,
  "status": "Ok"            // "Ok" | "Pending"
}
```

#### Get issue
```
GET /v1/issues/:id
Authorization: Bearer <token>
```

#### Update issue (partial)
```
PATCH /v1/issues/:id
Authorization: Bearer <token>
Content-Type: application/json

{
  "status": "Ok",
  "resolution": "Fixed by resetting the router"
}
```
Only the issue owner or a manager can update.

#### Delete issue
```
DELETE /v1/issues/:id
Authorization: Bearer <token>
```
Only the issue owner or a manager can delete.

---

### Dashboard Stats

```
GET /v1/stats?date=2026-03-21
Authorization: Bearer <token>
```

Response:
```json
{
  "stats": {
    "total_issues": 10,
    "resolved": 9,
    "pending": 1,
    "avg_minutes": 5.8,
    "by_type": {
      "Door": 6,
      "Internet": 3,
      "Hardware": 1
    },
    "by_technician": [
      { "user_id": 2, "name": "Alcidio", "avatar_idx": 2, "count": 7 },
      { "user_id": 1, "name": "Eder",    "avatar_idx": 1, "count": 4 }
    ]
  }
}
```

---

## Frontend

The LIRA frontend is **embedded directly into the binary** via `//go:embed`. Once the server is running, open:

```
http://localhost:4000
```

API calls use relative paths (`/v1/...`) so the app works on any host or port without any configuration. To update the UI, edit `ui/web/index.html` and rebuild.

---

## Build for Production

```bash
make build/api
# Binary at: ./bin/lira (current OS) and ./bin/linux_amd64/lira (Linux)
# The binary contains the entire app — frontend + API. Just copy it and run it.
```

Run in production:
```bash
./bin/api \
  -port=4000 \
  -env=production \
  -db-dsn=$LIRADB_DSN \
  -db-max-open-conns=25 \
  -db-max-idle-conns=25 \
  -limiter-rps=100 \
  -limiter-burst=200 \
  -cors-trusted-origins="https://yourdomain.com"
```

---

## Security Notes

- Passwords are hashed with **bcrypt** (cost 12)
- Auth tokens are **SHA-256 hashed** before storage — only the plaintext is returned once
- Tokens expire after **7 days**
- Rate limiting: 100 req/s per IP, burst 200 (configurable)
- Edit/delete permissions: owner or manager only
- All DB queries use **parameterised statements** (no SQL injection)
- Optimistic locking via `version` column prevents lost updates

---

## Useful Make Targets

```bash
make run/api          # Start the server
make run/api/dev      # Start without rate limiting
make db/psql          # Connect to DB with psql
make db/migrations/up # Apply all migrations
make db/migrations/new name=add_something  # Create new migration
make audit            # Format, vet, test
make build/api        # Compile binary
```
