# HarborWorks API Specification

## 1. Purpose and Scope
This document defines the implemented HTTP API for HarborWorks Booking Hub.

Audience:
- Backend and frontend engineers
- QA and acceptance reviewers
- Operations teams validating admin and governance paths

API style:
- REST-style JSON endpoints under `/api`
- Server-rendered HTML pages outside `/api`

## 2. Global Conventions

### 2.1 Content Types
- Request body: `application/json` unless explicitly multipart or file download.
- Response body: JSON by default, with binary document download endpoints and CSV export endpoint.

### 2.2 Authentication
- Session cookie: `harborworks_session`.
- Authentication is local username/password with server-side session resolution.
- Session inactivity behavior is sliding; cookie expiry is refreshed on authenticated requests.

### 2.3 Authorization
- User-level authorization enforced in API handlers/services.
- Admin-only routes are centralized under `/api/admin` and guarded by admin middleware.

### 2.4 Idempotency
- Header: `Idempotency-Key`.
- Used on selected write endpoints to provide at-most-once side effects and stable replay responses.
- Replay indicator header: `Idempotent-Replay: true`.

### 2.5 Caching
- Selected GET endpoints use read-through cache.
- Cache bypass for admin users: `X-Cache-Bypass: true`.

### 2.6 Error Envelope
Most errors return a JSON body with at least:
- `error`: human-readable message

Some endpoints return additional fields (`detail`, `hint`, `unlock_window`, etc.) depending on failure type.

## 3. Status Code Contract
- `200 OK`: successful read/update actions.
- `201 Created`: successful resource creation.
- `202 Accepted`: accepted asynchronous/scheduled behavior.
- `400 Bad Request`: malformed payload or validation failure.
- `401 Unauthorized`: missing/invalid authentication.
- `403 Forbidden`: authenticated but not permitted.
- `404 Not Found`: resource missing.
- `409 Conflict`: business-rule conflict, duplicate/idempotency conflict, state conflict.
- `423 Locked`: account lockout on auth failures.
- `424 Failed Dependency`: restore plan unavailable due to missing prerequisite backup.
- `500 Internal Server Error`: unexpected server failure.

## 4. Endpoint Catalog

### 4.1 Health
- `GET /healthz`
	- Purpose: liveness probe.
- `GET /readyz`
	- Purpose: readiness probe, includes DB reachability check.

### 4.2 Authentication
- `POST /api/auth/register`
	- Request: `username`, `password`.
	- Response: created user.
- `POST /api/auth/login`
	- Request: `username`, `password`, optional `captcha_token`, `captcha_answer`.
	- Response: authenticated user and session expiry metadata; sets session cookie.
- `POST /api/auth/logout`
	- Response: logout status; clears session cookie.
- `GET /api/auth/me`
	- Response: current user.
- `GET /api/auth/captcha`
	- Response: captcha token/question/expiry.
- `POST /api/auth/change-password`
	- Request: `current_password`, `new_password`.
	- Response: password rotation status.

### 4.3 Resources and Availability
- `GET /api/resources`
	- Response: resource list.
- `GET /api/resources/:id/remaining?start=<RFC3339>&end=<RFC3339>`
	- Response: capacity, active party size, remaining seats for the window.
- `GET /api/availability?resource_id=<uuid>&date=<YYYY-MM-DD>`
	- Response: per-slot availability and booked windows for the date.

### 4.4 Bookings
- `POST /api/bookings` (Idempotency recommended)
	- Request: `resource_id`, `start_time`, `end_time`, optional `group_id`, `party_size`, `notes`, `secure_notes`.
	- Policy gates: authentication, password rotation, blacklist block, booking policy checks.
- `GET /api/bookings`
	- Response: current user's bookings.
- `GET /api/bookings/:id`
	- Response: booking detail (owner-only).
- `POST /api/bookings/:id/transition` (Idempotency recommended)
	- Request: `target_state`.
	- Applies state-machine and cutoff rules.

### 4.5 Group Reservations
- `POST /api/groups`
	- Request: `name`, `organizer_name`, `organizer_email`, `capacity`, optional `notes`.
- `GET /api/groups`
	- Response: list, with PII masking according to viewer authorization.
- `GET /api/groups/:id`
	- Response: detail with masking policy applied.

### 4.6 Group Buys
- `POST /api/group-buys` (Idempotency recommended)
	- Request: `resource_id`, `title`, `capacity`, `starts_at`, `ends_at`, optional `description`, `threshold`, `deadline`.
- `GET /api/group-buys`
- `GET /api/group-buys/:id`
- `GET /api/group-buys/:id/progress`
	- Response includes threshold/confirmed/remaining/status and time-left fields.
- `GET /api/group-buys/:id/participants`
	- Response uses masked participant identity representation.
- `POST /api/group-buys/:id/join` (Idempotency recommended)
	- Request: optional `quantity` (defaults to 1).
	- Concurrency-safe via optimistic locking and idempotency.

### 4.7 Documents
- `POST /api/documents/confirmation` (Idempotency recommended)
	- Request: `related_type`, `related_id`, `title`, optional `fields`.
	- Generates or supersedes confirmation PDF revision.
- `POST /api/documents/checkin-pass` (Idempotency recommended)
	- Request shape same as above.
	- Generates or supersedes check-in pass PNG revision.
- `GET /api/documents`
- `GET /api/documents/:id`
	- Owner-only metadata and revision history.
- `GET /api/documents/:id/content?revision=<int>`
	- Owner-only binary retrieval.
	- Headers include revision metadata (`X-Revision`, optional `X-Superseded`).

### 4.8 Notifications and To-Dos
- `GET /api/notifications?unread=<0|1|true|false>&limit=<n>`
- `GET /api/notifications/unread-count`
- `POST /api/notifications/:id/read`
- `POST /api/todos`
	- Request: `task_type`, `title`, optional `payload`, optional `due_at`.
- `GET /api/todos?status=<open|in_progress|done|dismissed>&limit=<n>`
- `POST /api/todos/:id/status`
	- Request: `status`.

### 4.9 Analytics
- `POST /api/analytics/track`
	- Request: `event_type`, `target_type`, `target_id`.
- `GET /api/analytics/top?days=<n>&limit=<n>`
- `GET /api/analytics/trends?event_type=<view|favorite|comment|download>`
	- Returns `day_7`, `day_30`, `day_90` buckets.

### 4.10 Governance
- `GET /api/governance/dictionary`
- `GET /api/governance/tags`
- `POST /api/consent/grant`
	- Request: `scope`, optional `version` (default `v1`).
- `POST /api/consent/withdraw`
	- Request: `scope`, optional `version`.
- `GET /api/consent`
- `POST /api/account/delete`
	- Schedules hard deletion workflow.
- `POST /api/account/delete/cancel`

### 4.11 Admin
All admin endpoints require authenticated admin access.

Notifications and analytics:
- `GET /api/admin/notification-deliveries`
- `GET /api/admin/anomalies`

Governance and cache:
- `POST /api/admin/import/resources` (multipart form with file field `file`)
- `GET /api/admin/export/resources.csv`
- `GET /api/admin/cache/stats`
- `POST /api/admin/cache/purge`

Webhooks:
- `POST /api/admin/webhooks`
- `GET /api/admin/webhooks`
- `POST /api/admin/webhooks/:id/disable`
- `GET /api/admin/webhooks/deliveries`

Backups:
- `POST /api/admin/backups/full`
- `POST /api/admin/backups/incremental`
- `GET /api/admin/backups`
- `GET /api/admin/backups/restore-plan`

## 5. Security and Policy Guarantees
- Password policy, lockout, CAPTCHA, and inactivity are enforced server-side.
- Booking and group-buy writes include blacklist gating where required.
- Booking ownership and document ownership are enforced on read/write paths.
- Admin actions are guarded by centralized admin middleware.
- Idempotency prevents duplicate side effects for retried client actions.

## 6. Operational Notes
- Health and readiness endpoints are suitable for container orchestration checks.
- API is designed for offline/local-network operation.
- Restore execution is operationally supervised; API provides restore planning metadata rather than destructive restore execution.

## 7. Versioning and Compatibility
Current API is unversioned and intended for first-party UI consumption.

If external clients are introduced, a path or header versioning strategy should be added with explicit backward-compatibility policy.
