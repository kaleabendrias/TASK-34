# HarborWorks Delivery Acceptance & Project Architecture Audit (Static-Only)

Date: 2026-04-08

## 1. Verdict
- Overall conclusion: **Partial Pass**

Rationale:
- The repository is substantial and implements most requested capabilities end-to-end (auth, booking, group-buy, documents, notifications, analytics, governance, backups, webhooks), with broad static test presence.
- However, there are **material defects** including two **High** severity issues that affect requirement conformance and post-deletion reliability.

## 2. Scope and Static Verification Boundary
- Reviewed:
  - Project documentation and static consistency: `README.md`, `docker-compose.yml`, `docker-compose.test.yml`, `run_tests.sh`, `tests/entrypoint.sh`
  - Entry points and wiring: `cmd/server/main.go`, `internal/api/router.go`
  - Security and policy paths: auth/session/middleware, authorization gates, ownership checks
  - Core modules: booking/group-buy/document/notification/analytics/governance/backup/webhook services + repositories + migrations + views
  - Unit and API test suites (static inspection only): `unit_tests/*.go`, `API_tests/*.go`
- Not reviewed:
  - Runtime behavior in live environment, browser interaction timing, Docker/container runtime execution, DB runtime migrations, external network behavior.
- Intentionally not executed:
  - Project startup, Docker, tests, app endpoints.
- Claims requiring manual verification:
  - Runtime SLA claims (restore within 4 hours), real-time UI behavior under load, scheduler timing behavior, and actual operational backup media properties.

## 3. Repository / Requirement Mapping Summary
- Prompt core goal mapped:
  - Booking + group reservation hub with group-buy thresholds/deadlines and idempotent join outcomes.
  - Auth hardening (password policy, lockout, captcha, inactivity timeout).
  - Booking policy/state machine (lead-time/cutoff/daily cap/overlap/blacklist).
  - Documents with version/superseded history.
  - Notification + To-Do center + admin delivery logs.
  - Offline analytics with top/trend/anomaly rules.
  - Governance/privacy (dictionary/tags, consent, deletion, masking, encryption).
  - Local backups, file-based exchange, local webhooks with retry/backoff.
- Main implementation areas mapped:
  - HTTP + route policy: `internal/api/router.go`
  - Business logic: `internal/service/*.go`
  - Persistence/contracts: `internal/repository/*.go`, `migrations/*.sql`
  - Web UI (Templ): `internal/views/*.templ`
  - Static tests: `API_tests/*.go`, `unit_tests/*.go`

## 4. Section-by-section Review

### 4.1 Hard Gates

#### 4.1.1 Documentation and static verifiability
- Conclusion: **Partial Pass**
- Rationale:
  - Strong startup/test/config documentation exists and is mostly aligned with code layout.
  - One static inconsistency found in architecture docs listing only old migration set.
- Evidence:
  - Startup/test docs: `README.md:18`, `README.md:383`, `README.md:454`
  - Actual migrations present: `migrations/0001_initial.up.sql:1`, `migrations/0002_groupbuy_governance.up.sql:1`, `migrations/0003_finalization.up.sql:1`, `migrations/0004_hardening.up.sql:1`
  - Inconsistency: `README.md:454`
- Manual verification note:
  - Runtime instructions themselves were not executed (static-only boundary).

#### 4.1.2 Material deviation from prompt
- Conclusion: **Partial Pass**
- Rationale:
  - Core business scope is implemented.
  - Some requirement-fit deviations remain (notably session inactivity behavior and state-label UX wording).
- Evidence:
  - Scope present in routing/services: `internal/api/router.go:60`, `internal/service/booking.go:78`, `internal/service/groupbuy.go:51`, `internal/service/document.go:27`, `internal/service/governance.go:72`, `internal/service/analytics.go:84`

### 4.2 Delivery Completeness

#### 4.2.1 Core explicit requirements coverage
- Conclusion: **Partial Pass**
- Rationale:
  - Most explicit requirements are statically implemented.
  - High-impact gaps: inactivity timeout semantics likely violated client-side; organizer nullable migration conflicts with non-null model scan; state labels not user-friendly phrasing requested.
- Evidence:
  - Implemented controls: `internal/service/auth.go:36`, `internal/service/auth.go:38`, `internal/service/auth.go:39`, `internal/service/booking.go:108`, `internal/service/booking.go:117`, `internal/service/booking.go:126`, `internal/service/groupbuy.go:115`, `internal/repository/groupbuy.go:140`
  - Gaps: `internal/api/handlers/auth.go:85`, `internal/service/auth.go:233`, `internal/domain/groupbuy.go:44`, `internal/repository/groupbuy.go:269`, `migrations/0003_finalization.up.sql:23`, `internal/views/booking.templ:58`

#### 4.2.2 End-to-end deliverable completeness
- Conclusion: **Pass**
- Rationale:
  - Repository has complete layered structure, migrations, seed, runtime/test compose files, API and UI handlers, and broad tests.
- Evidence:
  - Entry/wiring: `cmd/server/main.go:33`, `internal/api/router.go:29`
  - Docs/tests layout: `README.md:383`, `run_tests.sh:1`, `tests/entrypoint.sh:1`
  - Feature breadth in handlers: `internal/api/router.go:81`, `internal/api/router.go:110`, `internal/api/router.go:124`, `internal/api/router.go:136`, `internal/api/router.go:150`, `internal/api/router.go:164`

### 4.3 Engineering and Architecture Quality

#### 4.3.1 Structure and module decomposition
- Conclusion: **Pass**
- Rationale:
  - Clean separation: handlers -> services -> repositories -> domain with explicit composition root.
- Evidence:
  - Dependency wiring: `cmd/server/main.go:81`, `cmd/server/main.go:148`, `internal/api/router.go:14`
  - Domain/service/repository split visible across `internal/domain`, `internal/service`, `internal/repository`.

#### 4.3.2 Maintainability/extensibility
- Conclusion: **Partial Pass**
- Rationale:
  - Generally maintainable with policy structs and interfaces.
  - Some brittle edges (nullability/model mismatch; scattered per-handler admin checks vs centralized policy) increase maintenance risk.
- Evidence:
  - Policy structs: `internal/service/auth.go:22`, `internal/service/booking.go:18`
  - Admin checks are handler-local: `internal/api/handlers/admin.go:26`, `internal/api/handlers/analytics.go:82`, `internal/api/handlers/governance.go:114`, `internal/api/handlers/notification.go:121`
  - Nullability mismatch risk: `migrations/0003_finalization.up.sql:23`, `internal/domain/groupbuy.go:44`, `internal/repository/groupbuy.go:269`

### 4.4 Engineering Details and Professionalism

#### 4.4.1 Error handling, logging, validation, API shape
- Conclusion: **Partial Pass**
- Rationale:
  - Strong validation and structured logging exist.
  - Some validation paths rely on DB errors and may surface as generic 500 rather than user-grade 4xx.
- Evidence:
  - Validation examples: `internal/service/booking.go:96`, `internal/service/groupbuy.go:88`, `internal/domain/password.go:21`
  - Error mapping default internal: `internal/api/handlers/booking.go:204`
  - Structured logs: `internal/infrastructure/logger/logger.go:9`, `internal/api/middleware/logger.go:14`

#### 4.4.2 Product-grade vs demo
- Conclusion: **Pass**
- Rationale:
  - Contains non-trivial production concerns: idempotency, optimistic locking, scheduled jobs, backups, encryption, deletion executor, admin controls, and broad tests.
- Evidence:
  - Idempotency middleware/repo: `internal/api/middleware/idempotency.go:54`, `internal/repository/idempotency.go:56`
  - Jobs: `cmd/server/main.go:127`
  - Encryption: `internal/infrastructure/crypto/aes.go:21`
  - Backup/webhook services: `internal/service/backup.go:21`, `internal/service/webhook.go:20`

### 4.5 Prompt Understanding and Requirement Fit

#### 4.5.1 Business goal and constraints fit
- Conclusion: **Partial Pass**
- Rationale:
  - Core scenario implemented with strong breadth.
  - Some semantics deviate from prompt intent (inactivity expiration behavior from client perspective; UX label phrasing; waitlist capacity accounting ambiguity).
- Evidence:
  - Core flows: `internal/api/router.go:84`, `internal/api/router.go:104`, `internal/api/router.go:113`, `internal/api/router.go:142`
  - Constraint implementation: `internal/service/auth.go:36`, `internal/service/auth.go:38`, `internal/service/booking.go:108`, `internal/service/groupbuy.go:22`
  - Deviations: `internal/api/handlers/auth.go:85`, `internal/views/booking.templ:58`, `internal/repository/resource.go:124`

### 4.6 Aesthetics (frontend/full-stack)

#### 4.6.1 Visual and interaction quality
- Conclusion: **Partial Pass**
- Rationale:
  - UI is coherent, responsive basics exist, and interaction feedback is present.
  - State labels are technical snake_case strings instead of clearly human-readable labels requested.
- Evidence:
  - Responsive/layout base: `internal/views/layout.templ:9`, `internal/views/notification.templ:26`
  - Interactions: `internal/views/booking.templ:66`, `internal/views/groupbuy.templ:151`, `internal/views/notification.templ:110`
  - Label issue: `internal/views/booking.templ:58`
- Manual verification note:
  - Actual rendered visual quality and cross-browser behavior require manual UI review.

## 5. Issues / Suggestions (Severity-Rated)

### Blocker / High

1) Severity: **High**  
Title: Session inactivity semantics are likely broken client-side (sliding server session, non-sliding cookie).  
Conclusion: **Fail**  
Evidence: `internal/service/auth.go:233`, `internal/repository/session.go:58`, `internal/api/handlers/auth.go:85`, `internal/api/handlers/auth.go:96`  
Impact:
- Prompt requires expiry after 30 minutes of inactivity. Current server-side expiry slides, but cookie `maxAge` is only set at login and is never refreshed on activity. Browser can drop cookie at ~30 minutes even during active use, causing forced re-auth and violating inactivity semantics.
- Affects user experience and strict requirement fit.
Minimum actionable fix:
- Refresh session cookie expiry on authenticated requests (or use session cookies + server-side expiry only) so client token lifetime tracks inactivity semantics.

2) Severity: **High**  
Title: Group-buy organizer nullability migration conflicts with non-null domain/repository scan.  
Conclusion: **Fail**  
Evidence: `migrations/0003_finalization.up.sql:23`, `migrations/0003_finalization.up.sql:27`, `internal/domain/groupbuy.go:44`, `internal/repository/groupbuy.go:269`  
Impact:
- Deletion flow explicitly allows `group_buys.organizer_id` to become NULL (`ON DELETE SET NULL`), but code scans into non-null `uuid.UUID`. Any such row can fail scan/read paths, breaking list/get/progress flows post-deletion.
- Undermines privacy/deletion requirement reliability and data integrity.
Minimum actionable fix:
- Make `OrganizerID` nullable in model/repository (`*uuid.UUID` or `sql.Null*`) and update JSON/handler logic accordingly.

### Medium

3) Severity: **Medium**  
Title: Waitlisted bookings are counted as active seat consumption in remaining-capacity calculations.  
Conclusion: **Partial Fail**  
Evidence: `internal/service/booking.go:155`, `internal/repository/resource.go:124`, `internal/repository/booking.go:138`  
Impact:
- Booking creation sets over-capacity requests to `waitlisted`, but resource remaining-seat aggregation includes `waitlisted` alongside confirmed/checked-in. This can depress displayed/derived remaining capacity and distort booking behavior.
- Potential semantic mismatch with expected waitlist behavior.
Minimum actionable fix:
- Exclude `waitlisted` from seat-consumption queries used for capacity/remaining-seat computations (or explicitly document and enforce a different intended model).

4) Severity: **Medium**  
Title: UI booking state labels are technical enums, not user-friendly clear labels required by prompt.  
Conclusion: **Partial Fail**  
Evidence: `internal/views/booking.templ:58`  
Impact:
- Prompt asks for clear labels such as “Pending Confirmation”, “Checked In”, etc. Current UI shows raw enum strings like `pending_confirmation`.
Minimum actionable fix:
- Add a presentation mapping function for friendly labels in Templ views.

5) Severity: **Medium**  
Title: Documentation architecture section is stale about migrations.  
Conclusion: **Fail (doc consistency)**  
Evidence: `README.md:454`; actual files: `migrations/0003_finalization.up.sql:1`, `migrations/0004_hardening.up.sql:1`  
Impact:
- Weakens static verifiability and can mislead auditors/operators about schema evolution.
Minimum actionable fix:
- Update architecture section to list all active migration files.

### Low

6) Severity: **Low**  
Title: Admin authorization is enforced per-handler rather than centrally at router group middleware.  
Conclusion: **Partial Risk**  
Evidence: `internal/api/router.go:154`, `internal/api/handlers/admin.go:26`, `internal/api/handlers/analytics.go:82`, `internal/api/handlers/governance.go:114`, `internal/api/handlers/notification.go:121`  
Impact:
- Current implementation appears correct, but future admin routes are easier to misconfigure if handler-level checks are forgotten.
Minimum actionable fix:
- Add a dedicated `RequireAdmin` middleware to `/api/admin` route group and keep handler checks as defense-in-depth.

## 6. Security Review Summary

- Authentication entry points: **Pass**
  - Evidence: `internal/api/router.go:69`, `internal/service/auth.go:113`, `internal/domain/password.go:21`
  - Notes: Password policy, lockout, captcha logic present.

- Route-level authorization: **Partial Pass**
  - Evidence: `internal/api/router.go:41`, `internal/api/router.go:84`, `internal/api/router.go:154`
  - Notes: Authn gates are broad and consistent. Admin role checks are mostly handler-level rather than route middleware.

- Object-level authorization: **Pass**
  - Evidence: `internal/api/handlers/booking.go:131`, `internal/service/booking.go:229`, `internal/api/handlers/document.go:99`, `internal/api/handlers/document.go:122`
  - Notes: Booking and document owner checks are explicit.

- Function-level authorization: **Pass**
  - Evidence: `internal/api/handlers/admin.go:26`, `internal/api/handlers/analytics.go:82`, `internal/api/handlers/notification.go:121`, `internal/api/handlers/governance.go:114`

- Tenant/user data isolation: **Partial Pass**
  - Evidence: `internal/repository/notification.go:56`, `internal/repository/notification.go:104`, `internal/repository/idempotency.go:52`
  - Notes: Isolation is generally strong; group-buy nullable organizer bug can break post-deletion reads.

- Admin/internal/debug protection: **Pass**
  - Evidence: `internal/api/router.go:154`, `API_tests/backup_test.go:54`, `API_tests/webhook_test.go:42`, `API_tests/analytics_test.go:51`

## 7. Tests and Logging Review

- Unit tests: **Pass**
  - Evidence: `unit_tests/password_test.go:11`, `unit_tests/state_test.go:24`, `unit_tests/csv_test.go:9`, `unit_tests/crypto_test.go:12`, `unit_tests/webhook_test.go:10`
  - Notes: Good coverage of pure logic/policy helpers.

- API/integration tests: **Partial Pass**
  - Evidence: `API_tests/auth_test.go:41`, `API_tests/booking_test.go:10`, `API_tests/groupbuy_test.go:122`, `API_tests/document_test.go:9`, `API_tests/governance_test.go:63`, `API_tests/db_failure_test.go:34`
  - Notes: Broad and robust suite exists; key uncovered defects remained (cookie sliding semantics, nullable organizer scan path).

- Logging categories/observability: **Pass**
  - Evidence: `internal/infrastructure/logger/logger.go:9`, `internal/api/middleware/logger.go:14`, `cmd/server/main.go:44`

- Sensitive-data leakage risk in logs/responses: **Partial Pass**
  - Evidence: no password logging in seed/auth path `cmd/server/main.go:251`, `internal/service/auth.go:161`; user object hides hash `internal/domain/models.go:39`
  - Notes: Request logger includes query string `internal/api/middleware/logger.go:19`; ensure no sensitive query params are introduced later.

## 8. Test Coverage Assessment (Static Audit)

### 8.1 Test Overview
- Unit tests exist: **Yes** (`unit_tests/*.go`).
- API/integration tests exist: **Yes** (`API_tests/*.go`).
- Framework: Go `testing` package.
- Test entry points and orchestration:
  - `run_tests.sh:1`
  - `tests/entrypoint.sh:1`
  - `docker-compose.test.yml:1`
- Documentation provides test command: `README.md:383`.

### 8.2 Coverage Mapping Table

| Requirement / Risk Point | Mapped Test Case(s) | Key Assertion / Fixture / Mock | Coverage Assessment | Gap | Minimum Test Addition |
|---|---|---|---|---|---|
| Password policy (12+/upper/lower/digit/symbol) | `unit_tests/password_test.go:11`, `API_tests/auth_test.go:9` | `ValidatePassword` fail/success checks; weak registration rejected | sufficient | None major | Add boundary test for exactly 12 chars in API path |
| Lockout after 5 fails + CAPTCHA from 3rd fail | `API_tests/auth_test.go:41` | 401 first fails, captcha required, then 423 lock | sufficient | None major | Add explicit assertion of 15m lock window metadata semantics |
| Session inactivity 30m behavior | (No direct test found) | N/A | **missing** | Sliding-cookie/client expiry mismatch not tested | Add API test that simulates activity beyond initial cookie max-age semantics |
| Booking lead time / overlap / transitions / owner-only | `API_tests/booking_test.go:10` | 409 lead-time + overlap; valid transitions; 403 for other user | basically covered | Daily limit and cutoff not directly asserted in API tests | Add API tests for cutoff window and 3-active/day cap |
| Booking blacklist blocks new bookings | `API_tests/extra_coverage_test.go` (exists but not statically inspected in detail here) | Partial indirect coverage likely | insufficient | No strong, explicit evidence extracted in this audit slice | Add dedicated blacklist booking create rejection test |
| Group-buy optimistic locking/oversell/idempotency | `API_tests/groupbuy_test.go:122`, `API_tests/idempotency_isolation_test.go:23` | concurrent join races, exactly one state change with same key | sufficient | None major | Add explicit failed-at-deadline slot-release verification |
| Group-buy defaults threshold/deadline | `API_tests/groupbuy_test.go:9` (create path) | Threshold/default behavior partially implied | insufficient | Default deadline=24h not directly asserted | Add assertion on default deadline value |
| Documents revision history + superseded + owner-only | `API_tests/document_test.go:9`, `API_tests/document_test.go:105` | revision increment, `superseded`, 403 for non-owner | sufficient | None major | Add check-in pass supersession revision test |
| Notification/Todo center + admin delivery logs | `API_tests/notifications_test.go:8`, `API_tests/notifications_test.go:42` | todo lifecycle, unread count, admin 403/200 | basically covered | Limited filtering edge cases | Add status filter edge + pagination tests |
| Analytics top + 7/30/90 + admin anomalies | `API_tests/analytics_test.go:8`, `API_tests/analytics_test.go:51` | trend keys, admin-only anomalies | basically covered | 3x anomaly rule itself not directly validated end-to-end | Add controlled fixture for baseline vs observed ratio rule |
| Governance dictionary/tags/consent/deletion/csv import-export | `API_tests/governance_test.go:15`, `API_tests/governance_test.go:35`, `API_tests/governance_test.go:63`, `unit_tests/csv_test.go:9` | consent lifecycle; deletion hard-delete checks; CSV all-or-nothing validation | basically covered | Deletion path does not assert group_buy nullable organizer read behavior | Add deletion-followed-by-group-buy-read regression test |
| Webhooks local-only validation + retry cap/backoff | `unit_tests/webhook_test.go:30`, `unit_tests/webhook_test.go:62`, `API_tests/webhook_test.go:8` | allowed/rejected URL checks; retry cap function; admin CRUD | sufficient | Delivery retry runtime cycles not deeply API-tested | Add integration test that forces repeated failed deliveries to `dead` after 5 |
| Backups full/incremental/list/restore-plan admin-only | `API_tests/backup_test.go:10`, `API_tests/backup_test.go:54` | full+incremental + restore-plan + 403 for non-admin | basically covered | 4-hour SLA cannot be proven statically by tests | Manual performance drill |

### 8.3 Security Coverage Audit
- Authentication: **Basically covered**
  - Evidence: `API_tests/auth_test.go:9`, `API_tests/auth_test.go:41`, `API_tests/misc_test.go:96`
  - Residual risk: inactivity cookie/sliding semantics untested.

- Route authorization: **Basically covered**
  - Evidence: `API_tests/backup_test.go:54`, `API_tests/webhook_test.go:42`, `API_tests/analytics_test.go:51`
  - Residual risk: future admin routes may miss per-handler checks without centralized middleware.

- Object-level authorization: **Covered for key objects**
  - Evidence: `API_tests/document_test.go:105`, `API_tests/booking_test.go:84`
  - Residual risk: not all object domains have equivalent explicit negative tests.

- Tenant/data isolation: **Basically covered**
  - Evidence: `API_tests/idempotency_isolation_test.go:91`, `API_tests/notifications_test.go:51`
  - Residual risk: nullable organizer scan defect can create availability/consistency issues post-deletion.

- Admin/internal protection: **Covered**
  - Evidence: `API_tests/backup_test.go:54`, `API_tests/webhook_test.go:42`, `API_tests/notifications_test.go:42`

### 8.4 Final Coverage Judgment
- **Partial Pass**

Boundary:
- Major happy paths and many failure/permission paths are covered.
- However, uncovered areas allow severe defects to persist while tests still pass (notably inactivity-cookie semantics and nullable organizer scan after deletion).

## 9. Final Notes
- This audit is static-only and evidence-based; no runtime claims are asserted as proven.
- Main remediation priority should be:
  1. Fix session inactivity semantics at cookie/server boundary.
  2. Resolve group-buy organizer nullability/model scan mismatch.
  3. Clarify waitlist capacity accounting and align UI state labels with business wording.
