# HarborWorks Booking & Group Reservation Hub

A self-contained, end-to-end booking platform for marina slips, moorings, and event spaces. Built with Go + Gin + Templ + PostgreSQL, packaged as a single Docker Compose stack with no host dependencies beyond Docker itself.

The full feature set includes local username/password auth with lockout and CAPTCHA, a server-side booking state machine, group-buy flows with optimistic locking, document generation with revision history, an in-app notification & to-do center, an offline analytics pipeline with anomaly detection, idempotency-key replay, encryption at rest for sensitive notes, self-service deletion, local webhooks with exponential backoff, and backup/restore against a named volume.

## Architecture & Tech Stack

* **Frontend:** Go Templ (server-side HTML rendering), vanilla JS, CSS custom properties
* **Backend:** Go 1.23, Gin HTTP framework
* **Database:** PostgreSQL 16 (pgx/v5 driver)
* **Containerization:** Docker & Docker Compose (Required)

## Project Structure

```text
.
├── cmd/server/             # Process entrypoint, wiring, graceful shutdown
├── internal/
│   ├── api/
│   │   ├── handlers/       # HTTP handlers (auth, booking, group, analytics, …)
│   │   ├── middleware/     # Authenticator, idempotency, read-through cache
│   │   └── router.go       # Route registration
│   ├── service/            # Business rules (auth, booking state machine, …)
│   ├── repository/         # pgx/v5 implementations of every interface
│   ├── domain/             # Pure models, validation, errors, state machine
│   ├── infrastructure/     # Config, database, logger, cache, crypto, jobs
│   └── views/              # Templ HTML templates (layout, auth, booking, …)
├── migrations/             # golang-migrate SQL files
├── seed/seed.sql           # Idempotent reference data
├── unit_tests/             # Pure-logic Go tests (≥90% scope coverage)
├── API_tests/              # End-to-end HTTP tests against live server
├── tests/entrypoint.sh     # In-container test runner
├── Dockerfile              # Production multi-stage build
├── Dockerfile.test         # Coverage-instrumented test image
├── docker-compose.yml      # Production stack (db + app)
├── docker-compose.test.yml # Test stack (db + test container)
├── run_tests.sh            # One-command test orchestrator — MANDATORY
└── README.md               # This file — MANDATORY
```

## Prerequisites

To ensure a consistent environment, this project is designed to run entirely within containers. You must have the following installed:
* [Docker](https://docs.docker.com/get-docker/)
* [Docker Compose](https://docs.docker.com/compose/install/)

No Go, templ, or PostgreSQL installation is required on the host.

## Running the Application

1. **Build and start containers:**
   ```bash
   docker-compose up --build
   ```
   > Docker Compose v2 alternative: `docker compose up --build`

   This builds the application image, starts PostgreSQL, runs all migrations, seeds reference data and demo users, generates the master encryption key on first boot, and starts the HTTP server on port `8088`.

2. **Verify the stack is healthy** (run after the containers report healthy):
   ```bash
   curl -sf http://localhost:8088/healthz | grep -q '"status":"alive"' && echo "live OK"
   curl -sf http://localhost:8088/readyz  | grep -q '"status":"ready"' && echo "ready OK"
   curl -s -o /dev/null -w "%{http_code}" -X POST http://localhost:8088/api/auth/login \
     -H 'Content-Type: application/json' \
     -d '{"username":"admin","password":"Admin@Harbor2026!"}' | grep -q 200 && echo "admin login OK"
   ```
   All three lines must print `OK`. Any other output indicates a startup problem.

3. **Access the app:**
   * UI: `http://localhost:8088/`
   * Login page: `http://localhost:8088/auth/login`
   * API: `http://localhost:8088/api/...`
   * Health: `http://localhost:8088/healthz`

4. **Stop the application:**
   ```bash
   docker-compose down        # keep data volumes
   docker-compose down -v     # wipe database, key, backup, and exchange volumes
   ```

## Testing

All unit, integration, and component tests are executed via a single standardized shell script. The script automatically handles container orchestration for the isolated test environment.

Make the script executable then run it:

```bash
chmod +x run_tests.sh
./run_tests.sh
```

The script:
1. Runs pure-logic unit tests (`unit_tests/`) with coverage scoped to `internal/domain`, `internal/service`, `internal/views`, `internal/api/middleware`, `internal/infrastructure/cache`, and `internal/infrastructure/crypto`
2. Starts a coverage-instrumented server binary
3. Runs end-to-end HTTP tests (`API_tests/`) against the live server
4. Enforces 90% coverage thresholds on both scopes independently

Exit codes:

| Code | Meaning |
| :--- | :--- |
| `0` | All tests passed and both coverage thresholds met |
| `1` | At least one test failed |
| `2` | Tests passed but a coverage threshold was missed |

Override thresholds if needed:

```bash
UNIT_THRESHOLD=85 API_THRESHOLD=80 ./run_tests.sh
```

## Seeded Credentials

The database is pre-seeded with the following users on startup (when `RUN_SEED=true`, which is the default in `docker-compose.yml`). Use these credentials to verify authentication and role-based access controls immediately after `docker compose up --build`.

| Role | Username | Password | Notes |
| :--- | :--- | :--- | :--- |
| **Admin** | `admin` | `Admin@Harbor2026!` | Full access to all system modules including admin endpoints. |
| **User** | `demouser` | `User@Harbor2026!` | Standard user with default permissions. |

Login at `http://localhost:8088/auth/login`.

> A third system account (`harbormaster`) is also seeded with a **randomly generated** password written to `/app/keys/initial_admin_password` inside the container. This account exists as a break-glass admin and requires password rotation on first login. The `admin` account above is the recommended entry point for reviewers.

## Booking State Machine

```
pending_confirmation ──▶ waitlisted ◀──▶ pending_confirmation
            │                                   │
            ├──▶ checked_in ──▶ completed       │
            │         │                         │
            └─────────┴────────▶ canceled ◀─────┘
```

Terminal states: `completed`, `canceled`. Every transition is enforced by `internal/domain/state.go`.

## Key Business Rules

| Rule | Enforced in | HTTP error |
| :--- | :--- | :--- |
| Password ≥ 12 chars, upper, lower, digit, symbol | `domain.ValidatePassword` | 400 |
| 30-minute inactivity session timeout (sliding) | `middleware.Authenticator` | 401 |
| 5 failed logins → 15-minute lockout | `service.AuthService.Login` | 423 |
| CAPTCHA required from 3rd failed attempt | `domain.User.CaptchaRequired` | 401 |
| Booking ≥ 2 h lead time | `service.BookingService.Create` | 409 |
| Max 3 active bookings per user per day | `service.BookingService.Create` | 409 |
| No overlapping bookings per user | `service.BookingService.Create` | 409 |
| Resource conflict → auto-waitlist | `service.BookingService.Create` | (status = `waitlisted`) |
| Hard blacklist blocks new bookings | `middleware.RequireNotBlacklisted` | 403 |
| Group-buy oversell prevention via optimistic lock | `repository.JoinAtomic` | 409 |
| Idempotency replay → identical response | `middleware.Idempotency` | (24 h TTL) |
| Cache TTL 60 s, admin bypass via `X-Cache-Bypass: true` | `middleware.ReadThroughCache` | – |

## Operational Notes

* **Fully offline.** Once the images are built, no external HTTP calls are made during normal operation.
* **No host paths, no env files.** Every runtime knob is declared inline in `docker-compose.yml` and all have sane defaults in `internal/infrastructure/config/config.go`.
* **Structured JSON logs** to stdout — collect with `docker compose logs app`.
* **Background jobs** run in-process via `jobs.Runner`: analytics aggregation (1 m), anomaly detection (5 m), group-buy sweep (1 m), deletion executor (5 m), webhook delivery (5 s), incremental backup (24 h), full backup (7 d).
* **Master encryption key** is generated on first boot at `/app/keys/master.key` (AES-256, mode 0600) and persists across restarts on the `harborworks_keys` named volume.
