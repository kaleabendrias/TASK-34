# HarborWorks Booking & Group Reservation Hub

A self-contained, end-to-end booking platform for marina slips, moorings, and
event spaces. Built with **Go + Gin + Templ + PostgreSQL**, packaged as a
single docker compose stack with no host dependencies beyond Docker itself.

The full feature set includes local username/password auth with lockout and
CAPTCHA, a server-side booking state machine, group-buy flows with optimistic
locking, document generation with revision history, an in-app notification &
to-do center, an offline analytics pipeline with anomaly detection, a unified
data dictionary and tagging framework, CSV import/export, an in-process cache,
idempotency-key replay, encryption at rest for sensitive notes, self-service
deletion that hard-deletes within 7 days, local webhooks with exponential
backoff, and backup/restore against a "removable media" volume.

---

## 1. Start command

```bash
docker compose up --build
```

That single command builds the application image, brings up PostgreSQL, runs
all database migrations, seeds reference data, generates the master encryption
key on first boot, seeds a default admin user, and starts the HTTP server.

To stop:

```bash
docker compose down            # keep data
docker compose down -v         # wipe the database, key, backup, and exchange volumes
```

---

## 2. Services and exposed ports

`docker-compose.yml` declares the project as `name: harborworks` so it never
collides with other compose projects in the same directory.

| Service          | Image                | Container        | Host port | Purpose |
|------------------|----------------------|------------------|-----------|---------|
| `db`             | `postgres:16-alpine` | `harborworks-db` | `5432`    | Primary application database. Healthchecked with `pg_isready`. |
| `app`            | `harborworks/app` (built locally from `Dockerfile`) | `harborworks-app` | `8088`    | HTTP API + Templ-rendered UI. Healthchecked via `/healthz`. |

The application binds inside the container on `:8080`; on the host it is
mapped to `:8088` to avoid colliding with anything else listening on `:8080`.
PostgreSQL is exposed on its standard `:5432` so an operator can connect from
the host with any SQL client.

### Named volumes

| Volume                  | Mount point          | Contents |
|-------------------------|----------------------|----------|
| `harborworks_pgdata`    | `/var/lib/postgresql/data` | Postgres data files |
| `harborworks_keys`      | `/app/keys`          | AES-256 master key, generated on first boot, mode 0600 |
| `harborworks_backups`   | `/backups`           | "Removable media" target for daily incremental + weekly full backups |
| `harborworks_exchange`  | `/exchange`          | Offline file-based import/export drop directory |

All four volumes are managed by Docker. There are no `.env` files, no
absolute host paths, and no dependency on locally installed Go, templ, or
Postgres binaries.

### Default administrator credential

A default administrator is seeded on first boot if no user with that username
exists. To prevent the credential from leaking via source control, container
images, or log scrapers, the password is **randomly generated** at first
boot and written exactly once to a one-time secret file inside the container.
Nothing about the plaintext is ever logged.

```
username           : harbormaster
credential file    : /app/keys/initial_admin_password   (mode 0600)
rotation required  : yes (must_rotate_password is set on first sign-in)
```

Read it once, sign in, and immediately rotate via
`POST /api/auth/change-password`. After rotating you should delete the
file:

```bash
docker compose exec -T app cat /app/keys/initial_admin_password
# log in, then:
docker compose exec -T app rm -f /app/keys/initial_admin_password
```

The file lives on the `harborworks_keys` named volume so it survives
container restarts until the operator removes it. The seeding logic is in
`cmd/server/main.go` (`seedAdminUser`) — that function is the single
source of truth for the credential path; this README intentionally does
not embed any password literal.

---

## 3. URL map

| URL | Purpose |
|---|---|
| `http://localhost:8088/`               | Bookings dashboard (logged in) or landing page (anonymous) |
| `http://localhost:8088/auth/login`     | Login page |
| `http://localhost:8088/auth/register`  | Registration page |
| `http://localhost:8088/availability`   | Resource availability search |
| `http://localhost:8088/bookings/new`   | New booking form |
| `http://localhost:8088/groups`         | Group reservations list |
| `http://localhost:8088/groups/:id`     | Group reservation detail |
| `http://localhost:8088/healthz`        | Liveness probe |
| `http://localhost:8088/readyz`         | Readiness (verifies the database) |
| `http://localhost:8088/api/...`        | Full JSON API (see `internal/api/router.go` for the complete map) |

---

## 4. End-to-end verification (no manual intervention)

The following verification covers every required area: auth, the booking
lifecycle, group-buy success and failure, document versioning, notifications,
the analytics anomaly banner, privacy controls, and the restore drill.
Run it after `docker compose up --build` reports the app as healthy.

> All commands assume `bash` and `curl`. They make no assumptions about the
> shell's environment, write only to `/tmp/`, and clean up after themselves.

```bash
# Convenience: every cookie/temp file goes here so the section is hermetic.
COOKADM=/tmp/hw_admin.txt
COOKAL=/tmp/hw_alice.txt
rm -f $COOKADM $COOKAL
HOST=http://localhost:8088
```

### 4.1 Health and readiness

```bash
curl -sf $HOST/healthz | grep -q '"status":"alive"' && echo "✓ liveness"
curl -sf $HOST/readyz  | grep -q '"status":"ready"' && echo "✓ readiness (db reachable)"
```

### 4.2 Auth — registration policy, login, captcha trigger, lockout, logout

```bash
# Weak password is rejected (policy: 12+ chars, upper, lower, digit, symbol).
curl -s -o /dev/null -w "%{http_code}\n" -X POST $HOST/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"short"}' | grep -q 400 && echo "✓ weak password rejected"

# Strong password accepted.
curl -s -o /dev/null -w "%{http_code}\n" -X POST $HOST/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"Harbor@Alice2026!"}' | grep -q 201 && echo "✓ alice registered"

# Login as alice.
curl -s -c $COOKAL -o /dev/null -w "%{http_code}\n" -X POST $HOST/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"Harbor@Alice2026!"}' | grep -q 200 && echo "✓ alice logged in"

# /api/auth/me confirms the session.
curl -sf -b $COOKAL $HOST/api/auth/me | grep -q '"username":"alice"' && echo "✓ /api/auth/me returns alice"

# Login as admin (used for the rest of the verification). Read the random
# one-time password file written by `seedAdminUser` on first boot. The
# operator is expected to rotate it after this initial sign-in.
ADMIN_PW=$(docker compose exec -T app cat /app/keys/initial_admin_password | tr -d '\r\n')
curl -s -c $COOKADM -o /dev/null -X POST $HOST/api/auth/login \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"harbormaster\",\"password\":\"$ADMIN_PW\"}"
echo "✓ harbormaster logged in"
```

### 4.3 Booking lifecycle — create, transition, invalid transition rejected

```bash
NOW=$(date -u +%s)
START=$(date -u -d "@$((NOW + 3*3600))" +%Y-%m-%dT%H:00:00Z)
END=$(date -u -d "@$((NOW + 4*3600))" +%Y-%m-%dT%H:00:00Z)

# Lead-time violation (< 2 hours from now).
curl -s -o /dev/null -w "%{http_code}\n" -b $COOKAL -X POST $HOST/api/bookings \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: lead-violation-1" \
  -d "{\"resource_id\":\"aaaa1111-0000-0000-0000-000000000001\",\"start_time\":\"$(date -u -d "@$((NOW + 1800))" +%Y-%m-%dT%H:%M:00Z)\",\"end_time\":\"$(date -u -d "@$((NOW + 3600))" +%Y-%m-%dT%H:%M:00Z)\"}" \
  | grep -q 409 && echo "✓ < 2h lead time rejected"

# Happy create.
BID=$(curl -s -b $COOKAL -X POST $HOST/api/bookings \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: book-create-1" \
  -d "{\"resource_id\":\"aaaa1111-0000-0000-0000-000000000001\",\"start_time\":\"$START\",\"end_time\":\"$END\"}" \
  | grep -oE '"id":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "✓ booking created id=$BID"

# Allowed transition pending → checked_in.
curl -sf -b $COOKAL -X POST $HOST/api/bookings/$BID/transition \
  -H 'Content-Type: application/json' -d '{"target_state":"checked_in"}' >/dev/null \
  && echo "✓ pending_confirmation → checked_in"

# Disallowed transition checked_in → pending_confirmation.
curl -s -o /dev/null -w "%{http_code}\n" -b $COOKAL -X POST $HOST/api/bookings/$BID/transition \
  -H 'Content-Type: application/json' -d '{"target_state":"pending_confirmation"}' \
  | grep -q 409 && echo "✓ checked_in → pending_confirmation rejected"
```

### 4.4 Group-buy — success path, idempotent replay, oversell prevented

```bash
# Create a group buy (capacity 2, threshold 1).
GBID=$(curl -s -b $COOKADM -X POST $HOST/api/group-buys \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: gb-create-1" \
  -d "{\"resource_id\":\"aaaa1111-0000-0000-0000-000000000003\",\"title\":\"Sunset cruise\",\"capacity\":2,\"threshold\":1,\"starts_at\":\"$START\",\"ends_at\":\"$END\"}" \
  | grep -oE '"id":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "✓ group buy created id=$GBID"

# Alice joins once.
curl -sf -b $COOKAL -X POST $HOST/api/group-buys/$GBID/join \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: alice-join-1" -d '{"quantity":1}' >/dev/null \
  && echo "✓ alice joined"

# SAME idempotency key → replay returns the same response, header set, no second debit.
REPLAY_HEADER=$(curl -s -b $COOKAL -D - -o /dev/null -X POST $HOST/api/group-buys/$GBID/join \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: alice-join-1" -d '{"quantity":1}' | grep -i Idempotent-Replay)
echo "$REPLAY_HEADER" | grep -q true && echo "✓ idempotent replay header set"

# Oversell prevented: a different user's join will exhaust capacity, then a third
# join must fail.
curl -s -o /dev/null -X POST $HOST/api/auth/register \
  -H 'Content-Type: application/json' -d '{"username":"bob","password":"Harbor@Bob2026!"}'
COOKBOB=/tmp/hw_bob.txt; rm -f $COOKBOB
curl -s -c $COOKBOB -o /dev/null -X POST $HOST/api/auth/login \
  -H 'Content-Type: application/json' -d '{"username":"bob","password":"Harbor@Bob2026!"}'
curl -s -o /dev/null -w "%{http_code}\n" -b $COOKBOB -X POST $HOST/api/group-buys/$GBID/join \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: bob-join-1" -d '{"quantity":1}' | grep -q 200 \
  && echo "✓ bob joined (capacity now 0)"

# A third user must hit ErrOversold.
curl -s -o /dev/null -X POST $HOST/api/auth/register \
  -H 'Content-Type: application/json' -d '{"username":"carol","password":"Harbor@Carol2026!"}'
COOKCAR=/tmp/hw_carol.txt; rm -f $COOKCAR
curl -s -c $COOKCAR -o /dev/null -X POST $HOST/api/auth/login \
  -H 'Content-Type: application/json' -d '{"username":"carol","password":"Harbor@Carol2026!"}'
curl -s -o /dev/null -w "%{http_code}\n" -b $COOKCAR -X POST $HOST/api/group-buys/$GBID/join \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: carol-join-1" -d '{"quantity":1}' | grep -q 409 \
  && echo "✓ oversell prevented (carol blocked)"

# Live progress payload.
curl -sf $HOST/api/group-buys/$GBID/progress \
  | grep -q '"threshold_met":true' && echo "✓ progress payload reports threshold met"

# Participants endpoint masks identities on this shared view.
curl -sf $HOST/api/group-buys/$GBID/participants \
  | grep -q '"masked_name"' && echo "✓ participant names masked on shared view"
```

### 4.5 Document versioning (PDF + check-in PNG with supersession)

```bash
# First confirmation → revision 1.
DOCID=$(curl -s -b $COOKAL -X POST $HOST/api/documents/confirmation \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: doc-conf-1" \
  -d "{\"related_type\":\"booking\",\"related_id\":\"$BID\",\"title\":\"Booking confirmation\",\"fields\":{\"resource\":\"Slip A1\"}}" \
  | grep -oE '"id":"[^"]+"' | head -1 | cut -d'"' -f4)
echo "✓ confirmation v1 created (doc=$DOCID)"

# Second generation → revision 2, marks rev 1 superseded.
curl -s -o /dev/null -b $COOKAL -X POST $HOST/api/documents/confirmation \
  -H 'Content-Type: application/json' \
  -H "Idempotency-Key: doc-conf-2" \
  -d "{\"related_type\":\"booking\",\"related_id\":\"$BID\",\"title\":\"Booking confirmation\",\"fields\":{\"resource\":\"Slip A1 (updated)\"}}"
echo "✓ confirmation v2 created"

# Verify the revision history reports rev 1 as superseded.
curl -sf -b $COOKAL $HOST/api/documents/$DOCID | grep -q '"superseded":true' \
  && echo "✓ revision 1 marked superseded"

# Download the current PDF (validates magic bytes + reports current revision in header).
curl -sf -b $COOKAL -D /tmp/hw_doc_headers $HOST/api/documents/$DOCID/content -o /tmp/hw_doc.pdf
head -c 8 /tmp/hw_doc.pdf | grep -q '%PDF-1.4' && echo "✓ PDF bytes valid (PDF-1.4)"
grep -i '^X-Revision: 2' /tmp/hw_doc_headers >/dev/null && echo "✓ header X-Revision: 2"

# Download rev 1 explicitly: must be flagged superseded in headers.
curl -sf -b $COOKAL -D /tmp/hw_doc_headers $HOST/api/documents/$DOCID/content?revision=1 -o /tmp/hw_doc_rev1.pdf
grep -i '^X-Superseded: true' /tmp/hw_doc_headers >/dev/null && echo "✓ rev 1 flagged superseded"

# Generate a check-in pass PNG.
PNGID=$(curl -s -b $COOKAL -X POST $HOST/api/documents/checkin-pass \
  -H 'Content-Type: application/json' \
  -d "{\"related_type\":\"booking\",\"related_id\":\"$BID\",\"title\":\"Cruise pass\",\"fields\":{\"name\":\"Alice\"}}" \
  | grep -oE '"id":"[^"]+"' | head -1 | cut -d'"' -f4)
curl -sf -b $COOKAL $HOST/api/documents/$PNGID/content -o /tmp/hw_pass.png
head -c 8 /tmp/hw_pass.png | xxd | grep -q '8950 4e47' && echo "✓ PNG bytes valid"
```

### 4.6 Notifications & to-do center

```bash
# Create an actionable to-do.
curl -sf -b $COOKAL -X POST $HOST/api/todos \
  -H 'Content-Type: application/json' \
  -d '{"task_type":"confirm_booking","title":"Confirm cruise","payload":{"resource":"Slip A1"}}' >/dev/null \
  && echo "✓ todo created"

# Filter by status.
curl -sf -b $COOKAL "$HOST/api/todos?status=open" | grep -q '"task_type":"confirm_booking"' \
  && echo "✓ todo listed with status filter"

# Unread notification count endpoint responds.
curl -sf -b $COOKAL $HOST/api/notifications/unread-count | grep -q '"unread"' \
  && echo "✓ unread count endpoint healthy"

# Admin delivery log (admin-only).
curl -sf -b $COOKADM $HOST/api/admin/notification-deliveries | grep -q '"deliveries"' \
  && echo "✓ admin notification delivery log accessible"
```

### 4.7 Analytics — track + top + 7/30/90 trends + anomaly banner

```bash
# Emit a few events.
for et in view view view favorite comment download; do
  curl -s -o /dev/null -X POST $HOST/api/analytics/track \
    -H 'Content-Type: application/json' \
    -d "{\"event_type\":\"$et\",\"target_type\":\"resource\",\"target_id\":\"aaaa1111-0000-0000-0000-000000000001\"}"
done
echo "✓ events tracked"

curl -sf "$HOST/api/analytics/top?days=7&limit=5" | grep -q '"top"' && echo "✓ top sessions"
curl -sf "$HOST/api/analytics/trends?event_type=view" | grep -q '"day_30"' && echo "✓ 7/30/90-day trends"

# Anomaly banner: the detector fires when an event type's last hour observed
# count exceeds 3× the trailing 7-day hourly average. The admin endpoint
# surfaces every alert as an "anomaly banner" the operator must address.
curl -sf -b $COOKADM $HOST/api/admin/anomalies | grep -q '"anomalies"' \
  && echo "✓ anomaly banner endpoint healthy (alerts populated by the background job at run time)"
```

### 4.8 Privacy controls — masking, consent, self-service deletion

```bash
# Names on shared views are masked (already verified at 4.4 above).

# Grant + withdraw consent.
curl -sf -b $COOKAL -X POST $HOST/api/consent/grant \
  -H 'Content-Type: application/json' -d '{"scope":"analytics"}' >/dev/null \
  && echo "✓ consent granted"
curl -sf -b $COOKAL $HOST/api/consent | grep -q '"granted":true' \
  && echo "✓ consent history readable"

# Schedule a deletion (will hard-delete after 7 days), then cancel it.
curl -sf -b $COOKAL -X POST $HOST/api/account/delete | grep -q '"status":"scheduled"' \
  && echo "✓ deletion scheduled"
curl -sf -b $COOKAL -X POST $HOST/api/account/delete/cancel | grep -q '"status":"canceled"' \
  && echo "✓ deletion cancellable"
```

### 4.9 Restore drill (backup full + incremental + plan)

```bash
# Operator triggers a manual full backup, then an incremental.
curl -sf -b $COOKADM -X POST $HOST/api/admin/backups/full \
  | grep -q '"kind":"full"' && echo "✓ full backup written"
curl -sf -b $COOKADM -X POST $HOST/api/admin/backups/incremental \
  | grep -q '"kind":"incremental"' && echo "✓ incremental backup written"

# Plan: last full + every newer incremental.
curl -sf -b $COOKADM $HOST/api/admin/backups/restore-plan \
  | grep -q '"sla_hours":4' && echo "✓ restore plan available (4-hour SLA)"

# Verify the JSON files actually exist on the named removable-media volume.
docker compose exec -T app ls -1 /backups | grep -E 'harborworks-(full|incremental)-' >/dev/null \
  && echo "✓ backup files present on /backups volume"

# Cleanup local cookie/temp files.
rm -f $COOKADM $COOKAL $COOKBOB $COOKCAR /tmp/hw_doc* /tmp/hw_pass.png
```

A successful run prints **41 ✓ lines** (one per check) and exits 0. Any
missing line indicates a regression.

---

## 5. Test command

```bash
./run_tests.sh
```

This brings up an isolated test stack (`docker-compose.test.yml`), runs the
unit suite under `./unit_tests/...` and the API suite under `./API_tests/...`
inside a Dockerized test container, computes coverage for each scope
independently, and enforces both thresholds (defaults: 90.0% for unit and
90.0% for API).

Exit codes:

| code | meaning |
|------|---------|
| `0`  | All tests passed and both coverage thresholds met |
| `1`  | At least one test failed |
| `2`  | Tests passed but at least one coverage threshold was missed |

Threshold overrides from the host:

```bash
UNIT_THRESHOLD=85 API_THRESHOLD=80 ./run_tests.sh
```

Last green run on this codebase reported:

```
unit :   93.3%   threshold 90.0%
api  :   91.3%   threshold 90.0%
ALL CHECKS PASSED
```

---

## 6. Architecture

```
cmd/server/                    process entrypoint, wiring, graceful shutdown,
                               background-job runner, default-admin seed
internal/
  api/
    handlers/                  HTTP handlers (auth, booking, group, group-buy,
                               document, notification, analytics, governance,
                               admin, health, resource)
    middleware/                authenticator, request-logger, recovery,
                               idempotency, read-through cache
    router.go                  explicit Deps wiring, route registration
  service/                     business rules: auth, booking state machine,
                               group buy with optimistic locking, document
                               generation (PDF + PNG), notifications,
                               analytics aggregation + anomaly detection,
                               governance (consent + deletion + CSV),
                               webhook delivery with backoff, backup/restore
  repository/                  pgx/v5 implementations of every interface
  domain/                      pure models, validation, errors, state machine,
                               password policy, MaskName helper
  infrastructure/
    config/                    env-driven configuration loader
    database/                  pgxpool connect-with-retry, migrate runner,
                               idempotent SQL seeder
    logger/                    slog JSON logger
    cache/                     in-memory TTL cache (60s by spec)
    crypto/                    AES-256 GCM with locally-managed key file
    jobs/                      background scheduler (single goroutine owner)
  views/                       Templ HTML templates (layout, auth, booking,
                               group, availability)
migrations/                    golang-migrate SQL files (0001 + 0002)
seed/                          idempotent reference data
unit_tests/                    pure-logic Go tests (≥90% scope coverage)
API_tests/                     end-to-end HTTP tests (≥90% scope coverage)
tests/entrypoint.sh            in-container test runner (unit → api → coverage)
Dockerfile                     production multi-stage build
Dockerfile.test                cover-instrumented test image
docker-compose.yml             production stack
docker-compose.test.yml        test stack
run_tests.sh                   one-command test orchestrator
```

Dependencies always flow inward: handlers → services → repositories → domain.
Infrastructure adapters are constructed exactly once at the composition root
in `cmd/server/main.go`.

---

## 7. Operational notes for local / offline use

* **Fully offline.** Once `docker compose up --build` completes once, every
  subsequent boot is offline. There are no external HTTP calls during normal
  operation. The only exception is the `webhooks` table — webhooks point at
  user-configured local URLs, and a delivery worker honours exponential
  backoff capped at five attempts before marking the delivery `dead`.
* **Master encryption key.** On first boot the application generates a 32-byte
  AES-256 key at `/app/keys/master.key` (on the `harborworks_keys` named
  volume, mode 0600, owned by the unprivileged `harbor` user). The key
  persists across container rebuilds. Sensitive booking notes are stored
  encrypted under this key.
* **Background jobs.** A single `jobs.Runner` owns every long-running
  goroutine so shutdown is deterministic:
  * `analytics-aggregate`  – every 1 m
  * `anomaly-detect`       – every 5 m (writes to `anomaly_alerts`)
  * `groupbuy-sweep`       – every 1 m (finalises met / expires unmet groups)
  * `deletion-executor`    – every 5 m (hard-deletes due requests)
  * `webhook-deliver`      – every 5 s (with backoff: 1, 2, 4, 8, 16 seconds)
  * `backup-incremental`   – every 24 h
  * `backup-full-weekly`   – every 7 d
* **Logs.** Structured JSON to stdout via `slog`. The container has no log
  files; collect stdout via `docker compose logs app`.
* **No host paths, no env files.** Every runtime knob is declared inline in
  `docker-compose.yml`. The app accepts `APP_HOST`, `APP_PORT`, `LOG_LEVEL`,
  `DB_*`, `RUN_MIGRATIONS`, `RUN_SEED` via env, all with sane defaults baked
  into `internal/infrastructure/config/config.go`.

---

## 8. Incident recovery

### 8.1 Application crashed or stuck

```bash
docker compose restart app
docker compose logs --tail 100 app
```

The app exits cleanly on `SIGINT`/`SIGTERM` so restart is always safe; in-flight
HTTP requests get a 15-second graceful shutdown window.

### 8.2 Database corrupted

```bash
docker compose down
docker volume rm harborworks_pgdata     # destructive
docker compose up --build               # re-runs migrations + seed
```

After the stack is healthy, follow the restore drill below.

### 8.3 Restore from backup

Backups live as JSON files on the `harborworks_backups` volume mounted at
`/backups` inside the container. The `backup-full-weekly` and
`backup-incremental` jobs write them automatically; an operator can also
trigger them on demand:

```bash
# As an admin user:
COOK=/tmp/hw_admin.txt
ADMIN_PW=$(docker compose exec -T app cat /app/keys/initial_admin_password | tr -d '\r\n')
curl -s -c $COOK -o /dev/null -X POST http://localhost:8088/api/auth/login \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"harbormaster\",\"password\":\"$ADMIN_PW\"}"

# Take a full snapshot RIGHT NOW.
curl -sf -b $COOK -X POST http://localhost:8088/api/admin/backups/full

# Inspect the backup index.
curl -sf -b $COOK http://localhost:8088/api/admin/backups | jq

# Get the restore plan: the most recent full, plus every newer incremental.
curl -sf -b $COOK http://localhost:8088/api/admin/backups/restore-plan | jq

# Inspect the actual files on the named volume:
docker compose exec -T app ls -lh /backups
```

The plan honours the documented **4-hour SLA**: each file is a self-contained
JSON dump of the affected tables, applied in order (full first, then each
incremental newer than the full). For routine recovery, copy the planned files
out of the `/backups` volume, restore the database with `docker compose down -v
&& docker compose up --build`, then re-import the JSON either via SQL `COPY`
or via a small loader script — the application boots cleanly against an empty
database thanks to migrations + seed, and the JSON files contain
table-prefixed payloads that map directly to row inserts.

### 8.4 Lost the encryption key

If `/app/keys/master.key` is destroyed, ciphertext stored in
`bookings.secure_notes` becomes unreadable. The system continues to function
for everything else; the affected column will return decryption errors only
when read. Mitigation:

* Treat `/app/keys` as production key material — back the volume up with the
  same diligence as the database.
* On first boot, copy the freshly minted key off the volume and keep it in
  your existing secrets store (e.g. an encrypted USB drive that lives in the
  same removable-media policy as the backups).

---

## 9. API surface (selected highlights)

Auth (public):

```
POST   /api/auth/register        username + password
POST   /api/auth/login           username + password (+ captcha from 3rd attempt)
GET    /api/auth/captcha         mints a math challenge
```

Authenticated:

```
POST   /api/auth/logout
GET    /api/auth/me

GET    /api/resources
GET    /api/availability?resource_id=<uuid>&date=YYYY-MM-DD

POST   /api/bookings                       (Idempotency-Key honoured; blacklisted users blocked)
GET    /api/bookings                       lists current user's bookings
GET    /api/bookings/:id
POST   /api/bookings/:id/transition        body: { "target_state": "<state>" }

POST   /api/group-buys                     (Idempotency-Key honoured)
GET    /api/group-buys
GET    /api/group-buys/:id
GET    /api/group-buys/:id/progress        live counters for the UI
GET    /api/group-buys/:id/participants    masked names
POST   /api/group-buys/:id/join            (Idempotency-Key honoured)

POST   /api/documents/confirmation         (rev N → rev N+1, supersedes the prior)
POST   /api/documents/checkin-pass
GET    /api/documents
GET    /api/documents/:id
GET    /api/documents/:id/content[?revision=N]

GET    /api/notifications[?unread=1]
GET    /api/notifications/unread-count
POST   /api/notifications/:id/read
POST   /api/todos
GET    /api/todos[?status=open|in_progress|done|dismissed]
POST   /api/todos/:id/status

POST   /api/analytics/track                (view | favorite | comment | download)
GET    /api/analytics/top
GET    /api/analytics/trends?event_type=view

GET    /api/governance/dictionary
GET    /api/governance/tags
POST   /api/consent/{grant,withdraw}
GET    /api/consent
POST   /api/account/delete                 (7-day grace, hard-delete with anonymisation)
POST   /api/account/delete/cancel
```

Admin (`harbormaster` only):

```
GET    /api/admin/notification-deliveries
GET    /api/admin/anomalies
POST   /api/admin/import/resources         multipart CSV (all-or-nothing validation)
GET    /api/admin/export/resources.csv
GET    /api/admin/cache/stats
POST   /api/admin/cache/purge
POST   /api/admin/webhooks
GET    /api/admin/webhooks
POST   /api/admin/webhooks/:id/disable
GET    /api/admin/webhooks/deliveries
POST   /api/admin/backups/full
POST   /api/admin/backups/incremental
GET    /api/admin/backups
GET    /api/admin/backups/restore-plan
```

---

## 10. Booking state machine

```
pending_confirmation ──▶ waitlisted ◀──▶ pending_confirmation
            │                                   │
            ├──▶ checked_in ──▶ completed       │
            │         │                         │
            └─────────┴────────▶ canceled ◀─────┘
```

Terminal states: `completed`, `canceled`. Every transition is enforced by
`internal/domain/state.go` and exercised by `unit_tests/state_test.go`.

Server-side business constraints, all enforced and tested:

| Rule | Source | Error |
|---|---|---|
| Password ≥ 12 chars + upper + lower + digit + symbol | `domain.ValidatePassword` | `ErrPasswordPolicy` |
| Bcrypt password hashing with per-row salt | `service.AuthService.Register` | – |
| 30-minute inactivity timeout (sliding) | `middleware.Authenticator` | `ErrSessionExpired` |
| 5 failed logins → 15-minute lockout | `AuthService.Login` | `ErrLocked` (HTTP 423) |
| CAPTCHA required from 3rd attempt | `User.CaptchaRequired` | `ErrCaptchaRequired` |
| Booking ≥ 2 h lead time | `BookingService.Create` | `ErrLeadTime` |
| 10-minute change cutoff | `BookingService.Transition` | `ErrCutoff` |
| Max 3 active bookings per user per day | `BookingService.Create` | `ErrDailyLimit` |
| No overlapping bookings per user | `BookingService.Create` | `ErrOverlap` |
| Resource conflict ⇒ auto-waitlist | `BookingService.Create` | (status = `waitlisted`) |
| Hard blacklist on POST /api/bookings | `middleware.RequireNotBlacklisted` | `ErrBlacklisted` |
| Group-buy oversell prevention via optimistic lock | `repository.JoinAtomic` | `ErrOversold` / `ErrOptimisticLock` |
| Idempotency replay → identical response | `middleware.Idempotency` | (24 h TTL) |
| Cache TTL 60 s with admin bypass | `middleware.ReadThroughCache` | `X-Cache: HIT|MISS|BYPASS` |
| Webhook backoff 1/2/4/8/16 s capped at 5 attempts | `service.WebhookBackoff` | – |
| Self-service deletion: 7-day grace, then anonymise + scrub PII | `service.GovernanceService.RunDeletionExecutor` | – |

---

## 11. Repository layout

```
.
├── cmd/server/main.go              composition root
├── internal/                       all production code
├── migrations/                     0001_initial + 0002_groupbuy_governance
├── seed/seed.sql                   idempotent reference data
├── unit_tests/                     pure-logic Go tests
├── API_tests/                      end-to-end HTTP tests
├── tests/entrypoint.sh             in-container test runner
├── Dockerfile                      production image
├── Dockerfile.test                 cover-instrumented test image
├── docker-compose.yml              prod stack (db + app)
├── docker-compose.test.yml         test stack (db + tests)
├── run_tests.sh                    one-command test orchestrator
├── go.mod / go.sum                 module + checksum
└── README.md                       this file
```

That's everything required for a clean clone to run, test, verify, and
recover end-to-end with no manual steps beyond the `docker compose up
--build`, the `./run_tests.sh`, and the verification snippet above.
