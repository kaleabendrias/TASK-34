# HarborWorks Booking Hub - Design Document

## 1. Purpose
This document describes the production architecture and design rationale of the HarborWorks Booking and Group Reservation Hub as implemented in code. It is intended for engineering handover, operations, and quality review.

## 2. System Scope
HarborWorks is an offline-first booking platform for limited-capacity sessions, with optional group-buy workflows, generated booking documents, notifications, analytics, governance controls, and backup/restore tooling.

Core stack:
- Backend: Go, Gin
- UI: Templ-rendered server-side pages
- Persistence: PostgreSQL
- Runtime shape: single app process + PostgreSQL, with background jobs inside app process

## 3. Architecture Overview

### 3.1 Layered Design
The codebase follows a layered architecture with explicit composition in the process entrypoint:
- Entry/composition root: `cmd/server/main.go`
- Transport: `internal/api/handlers` and `internal/api/router.go`
- Cross-cutting middleware: `internal/api/middleware`
- Business services: `internal/service`
- Persistence adapters: `internal/repository`
- Domain model and invariants: `internal/domain`
- Infrastructure adapters: cache, logger, crypto, config, database, jobs under `internal/infrastructure`

Dependencies flow inward only (handler -> service -> repository/domain), and all concrete wiring occurs in one place (`main.go`).

### 3.2 Runtime Components
- HTTP server with graceful shutdown and health endpoints.
- PostgreSQL connection pool.
- In-process TTL read-through cache.
- In-process background job runner.
- Local key manager for encryption at rest.

## 4. Request Pipeline

### 4.1 Global Middleware
Applied globally:
- Panic recovery middleware.
- Structured request logging middleware.

### 4.2 Authentication and Authorization Pipeline
Auth model is cookie-based server session:
- Session cookie name: `harborworks_session`.
- Required and optional authentication modes are both supported by route groups.
- Inactivity window is enforced server-side and cookie expiry is refreshed on authenticated requests.

Authorization gates:
- `RequirePasswordRotated`: blocks normal operations until forced password change is completed.
- `RequireNotBlacklisted`: blocks booking/group-buy write actions for blacklisted users.
- `RequireAdmin`: centralized admin guard for `/api/admin` routes.

### 4.3 Reliability Middleware
- Idempotency middleware for write endpoints requiring at-most-once semantics.
	- Header: `Idempotency-Key`.
	- Per-user key scope.
	- Stores canonical response for replay.
	- Returns conflict for in-flight duplicate requests.
- Read-through cache middleware for selected GET endpoints.
	- Admin bypass header: `X-Cache-Bypass: true`.

## 5. Domain and Business Rules

### 5.1 Authentication and Session Security
- Local username/password only.
- Password policy enforced in domain validator (length and complexity).
- Failed login tracking with lockout window.
- CAPTCHA challenge required after repeated failures.
- Session inactivity timeout with sliding activity updates.

### 5.2 Booking Lifecycle
- Booking states: pending confirmation, waitlisted, checked in, completed, canceled.
- State transitions enforced via server-side state machine.
- Policy constraints enforced in service layer:
	- Minimum lead time.
	- Cutoff window before start.
	- Daily active booking cap per user.
	- User overlap prevention.
	- Blacklist hard-block.
- Sensitive booking notes are encrypted before persistence.

### 5.3 Group-Buy Lifecycle
- Group-buy status includes open/met/finalized/expired/canceled/failed.
- Seat reservation uses optimistic locking in repository transaction.
- Join deduplication combines database uniqueness and idempotency middleware.
- Deadline sweep job finalizes successful campaigns and marks unmet campaigns failed with slot release.

### 5.4 Documents
- Generated document types: confirmation PDF and check-in pass PNG.
- Revision model:
	- Append-only revisions.
	- Previous revision marked superseded on new revision.
	- Current and historical content retrievable by revision.
- Owner-only access enforcement at handler level.

### 5.5 Notifications and To-Do
- In-app notifications with unread counters.
- To-do items with status transitions and filtering.
- Delivery log endpoints exposed for admin audit.

### 5.6 Analytics
- Event ingestion endpoint for view/favorite/comment/download events.
- Query endpoints for top sessions and 7/30/90-day trends.
- Anomaly detection compares latest hour against trailing 7-day baseline.
- Admin endpoint exposes anomaly alerts.

### 5.7 Governance and Privacy
- Data dictionary and tags endpoints.
- Consent grant/withdraw/history.
- Self-service account deletion request and cancellation.
- Deletion executor hard-deletes personal data and anonymizes retained analytics.
- CSV import/export for resources with bulk validation and all-or-nothing behavior.

### 5.8 Integrations and Operations
- Webhook management with local-network validation and bounded retries.
- Backup endpoints for full and incremental snapshots.
- Restore-plan endpoint returns ordered artifacts for recovery.

## 6. Data Model Summary
Implemented through SQL migrations in `migrations/`.

Primary entities:
- users, sessions, captcha_challenges
- resources, bookings, group_reservations
- group_buys, group_buy_participants
- idempotency_keys
- documents, document_revisions
- notifications, todos, notification_deliveries
- analytics_events, analytics_hourly, anomaly_alerts
- data_dictionary, tags, taggings, consent_records, deletion_requests
- webhooks, webhook_deliveries
- backups

Key structural properties:
- Foreign keys and deletion semantics are used to support hard-delete privacy flows.
- Check constraints enforce state and shape invariants at database level.
- Unique constraints support deduplication (for example, group-buy participation uniqueness).

## 7. Background Job Topology
Jobs are scheduled in-process by a single runner owned by the main process:
- analytics aggregation
- anomaly detection
- group-buy expiration sweep
- deletion executor
- webhook delivery cycle
- incremental backup scheduler
- weekly full backup scheduler

This design keeps deployment simple for offline/single-site operation while centralizing control and observability.

## 8. Security Design Summary
- Session cookie is HttpOnly and secure-by-default.
- Password hashing uses bcrypt.
- Forced password rotation is supported for seeded admin bootstrap.
- Admin access is route-group gated.
- Object ownership checks are applied to user-scoped resources.
- Sensitive notes are encrypted at rest with locally managed AES-GCM key material.
- Webhook target validation constrains delivery to local/private network ranges.

## 9. Observability and Operations
- JSON structured logs via slog.
- Liveness endpoint (`/healthz`) and readiness endpoint (`/readyz`).
- Recovery middleware prevents process crashes from handler panics.
- Graceful shutdown with timeout budget is implemented in server lifecycle.

## 10. Testing and Quality Strategy
- Unit tests for domain and infrastructure logic.
- API tests for end-to-end behavior, authorization boundaries, idempotency, and failure branches.
- Coverage thresholds enforced by test runner scripts.

## 11. Known Architectural Boundaries
- Single-process scheduler is appropriate for current offline deployment model; horizontal multi-writer scheduling would need leader election.
- Restore execution is intentionally operational/manual; API exposes restore plan but not destructive restore execution.
- Public API versioning is not introduced yet because clients are first-party UI and local operators.
