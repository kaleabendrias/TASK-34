# HarborWorks Delivery Acceptance and Project Architecture Audit (Static-Only)

Date: 2026-04-08

## 1. Verdict
- Overall conclusion: **Partial Pass**

The repository is substantial and maps to most required domains (auth, booking, group-buy, documents, notifications/todos, analytics, governance, backups, webhooks). However, there are material defects and requirement deviations, including one security-high idempotency flaw, one documentation hard-gate contradiction, and several requirement-fit gaps.

## 2. Scope and Static Verification Boundary
- Reviewed:
  - Documentation and startup/test instructions: `README.md`, `run_tests.sh`
  - Entrypoints, routing, middleware, handlers, services, repositories, migrations
  - Templ UI views
  - Unit/API test sources and test orchestration files
- Not reviewed:
  - Runtime behavior in a live environment
  - Container/network interactions, actual DB restores, browser rendering fidelity under real execution
- Intentionally not executed:
  - Project startup, Docker, tests, external services (per audit constraints)
- Claims requiring manual verification:
  - Real-time concurrency behavior under production load
  - Actual backup/restore RTO <= 4 hours
  - Webhook delivery against real endpoints and DNS/network edge cases
  - End-to-end browser interaction/performance

## 3. Repository / Requirement Mapping Summary
- Prompt core goal mapped: booking + group reservations + group-buy discount threshold + document revisioning + notification center + admin analytics + governance/privacy + offline integrations.
- Primary implementation areas mapped:
  - API/routing: `internal/api/router.go`
  - Security/auth/session: `internal/service/auth.go`, `internal/api/middleware/auth.go`
  - Booking/group-buy core logic: `internal/service/booking.go`, `internal/service/groupbuy.go`, `internal/repository/groupbuy.go`
  - Governance/privacy: `internal/service/governance.go`, migrations `0002`/`0003`
  - Offline integrations/backups: `internal/service/webhook.go`, `internal/service/backup.go`
  - UI (Templ): `internal/views/*.templ`
  - Tests: `API_tests/*.go`, `unit_tests/*.go`

## 4. Section-by-section Review

### 4.1 Hard Gates

#### 4.1.1 Documentation and static verifiability
- Conclusion: **Partial Pass**
- Rationale:
  - Startup/run/test instructions and project shape are present and detailed.
  - A material static contradiction exists for admin bootstrap credentials.
- Evidence:
  - Startup/test docs: `README.md:13`, `README.md:368`, `run_tests.sh:1`
  - Contradiction:
    - README claims fixed admin password `Harbor@Works2026!`: `README.md:72`, `README.md:145`
    - Code generates random one-time password to file: `cmd/server/main.go:223`, `cmd/server/main.go:266`, `cmd/server/main.go:205`
- Manual verification note:
  - Runtime bootstrap behavior must be checked manually because docs and code disagree.

#### 4.1.2 Material deviation from Prompt
- Conclusion: **Partial Pass**
- Rationale:
  - Most major features exist.
  - Group-buy failure semantics diverge (`expired` instead of required `Failed`), and slot release is not implemented as an explicit backend invariant.
- Evidence:
  - Group-buy states use `open|met|expired|canceled|finalized`: `migrations/0002_groupbuy_governance.up.sql:27`, `internal/domain/groupbuy.go:14`
  - Expiry sweep sets terminal status only, no resource-slot release logic: `internal/service/groupbuy.go:174`, `internal/repository/groupbuy.go:182`
  - UI text claims release, but service/repository have no corresponding resource reconciliation: `internal/views/groupbuy.templ:124`

### 4.2 Delivery Completeness

#### 4.2.1 Core requirements coverage
- Conclusion: **Partial Pass**
- Rationale:
  - Implemented: local auth, password policy, lockout/captcha, booking policy/state machine, group-buy threshold/deadline defaults, documents with revisions/superseded markers, notifications/todos, analytics trends/anomaly, consent/deletion, webhooks with retry cap, cache TTL/admin bypass, CSV import/export, encrypted notes.
  - Gaps/deviations: explicit `Failed` group-buy terminal state not present; CSV validation does not include date-field validation as specified.
- Evidence:
  - Auth policy: `internal/service/auth.go:35`, `internal/service/auth.go:37`, `internal/service/auth.go:38`, `internal/domain/password.go:22`
  - Booking policy/state: `internal/service/booking.go:26`, `internal/service/booking.go:132`, `internal/service/booking.go:138`, `internal/domain/state.go:6`
  - Group-buy defaults: `internal/service/groupbuy.go:21`, `internal/service/groupbuy.go:22`
  - Document revisions/superseded: `internal/api/handlers/document.go:107`, `internal/views/document.templ:50`
  - Notification/to-do center + filters: `internal/views/notification.templ:57`
  - Analytics anomaly threshold 3x: `internal/service/analytics.go:22`, `internal/service/analytics.go:102`
  - Cache TTL/admin bypass: `internal/infrastructure/cache/cache.go:9`, `internal/api/middleware/cache.go:14`, `internal/api/middleware/cache.go:26`
  - CSV import columns (no date field): `internal/service/governance.go:199`, `internal/service/governance.go:242`

#### 4.2.2 0-to-1 deliverable completeness
- Conclusion: **Pass**
- Rationale:
  - Full multi-module project with migrations, seed, handlers/services/repositories, views, test suites, and compose/test orchestration.
- Evidence:
  - Entrypoint wiring: `cmd/server/main.go:33`, `cmd/server/main.go:171`
  - Route map breadth: `internal/api/router.go:93`, `internal/api/router.go:113`, `internal/api/router.go:124`, `internal/api/router.go:160`
  - Tests present: `README.md:435`, `README.md:436`, `run_tests.sh:1`

### 4.3 Engineering and Architecture Quality

#### 4.3.1 Structure and module decomposition
- Conclusion: **Pass**
- Rationale:
  - Clear layering (handlers/services/repositories/domain/middleware/views), explicit dependency wiring, migrations and infra packages separated.
- Evidence:
  - Explicit dependency graph construction: `cmd/server/main.go:89`, `cmd/server/main.go:150`
  - Router dependency container: `internal/api/router.go:17`

#### 4.3.2 Maintainability/extensibility
- Conclusion: **Partial Pass**
- Rationale:
  - Generally maintainable design and typed domain errors.
  - Idempotency mechanism is not architected for robust at-most-once semantics under concurrency and cross-user key collisions.
- Evidence:
  - Middleware reads by key only, no user scoping: `internal/api/middleware/idempotency.go:75`, `internal/repository/idempotency.go:29`
  - Persist-after-handler with ignored write conflict: `internal/api/middleware/idempotency.go:108`, `internal/repository/idempotency.go:47`

### 4.4 Engineering Details and Professionalism

#### 4.4.1 Error handling/logging/validation/API design
- Conclusion: **Partial Pass**
- Rationale:
  - Good structured logging and domain-driven HTTP mapping exist.
  - Sensitive and security details are mixed: session cookie lacks Secure flag; request logs include raw query strings (possible leakage risk depending on use).
- Evidence:
  - Structured request logging: `internal/api/middleware/logger.go:20`
  - Session cookie set with `secure=false`: `internal/api/handlers/auth.go:81`
  - Error mapping: `internal/api/handlers/booking.go:196`, `internal/api/handlers/auth.go:140`

#### 4.4.2 Real product/service shape vs demo
- Conclusion: **Pass**
- Rationale:
  - Repository shape and breadth strongly indicate product-like scope rather than a single-feature demo.
- Evidence:
  - Background jobs and operational modules: `cmd/server/main.go:127`
  - Backups/webhooks/governance/admin endpoints: `internal/api/router.go:160`

### 4.5 Prompt Understanding and Requirement Fit

#### 4.5.1 Business goal and constraint fit
- Conclusion: **Partial Pass**
- Rationale:
  - Core business flows are mostly understood and implemented.
  - Notable fit issues remain in idempotency guarantees and group-buy failure semantics.
- Evidence:
  - At-most-once requirement risk: `internal/api/middleware/idempotency.go:75`, `internal/api/middleware/idempotency.go:108`, `internal/repository/idempotency.go:47`
  - Group-buy failed-state mismatch: `internal/domain/groupbuy.go:14`, `migrations/0002_groupbuy_governance.up.sql:27`, `internal/views/groupbuy.templ:71`

### 4.6 Aesthetics (frontend)

#### 4.6.1 Visual/interaction quality fit
- Conclusion: **Pass**
- Rationale:
  - Functional areas are distinct, responsive behaviors exist, state badges/feedback present, and key pages are coherent.
  - Fine-grained UX polish (human-readable state labels in all contexts) is partial.
- Evidence:
  - Responsive layout and component styling: `internal/views/layout.templ:10`, `internal/views/notification.templ:27`
  - Group-buy progress/deadline/status cues: `internal/views/groupbuy.templ:89`, `internal/views/groupbuy.templ:108`, `internal/views/groupbuy.templ:116`
  - Document superseded badge: `internal/views/document.templ:50`

## 5. Issues / Suggestions (Severity-Rated)

### Blocker / High

1) **High** - Idempotency is not safe for concurrent same-key requests (at-most-once violation risk)
- Conclusion: **Fail**
- Evidence:
  - Lookup then execute path: `internal/api/middleware/idempotency.go:75`
  - Record persisted only after handler completes: `internal/api/middleware/idempotency.go:108`
  - Conflict on insert is ignored by caller; DB uses `ON CONFLICT DO NOTHING`: `internal/repository/idempotency.go:47`
- Impact:
  - Two in-flight retries with same key can both execute side effects before one record is stored, violating required at-most-once effects.
- Minimum actionable fix:
  - Reserve key atomically before executing handler (e.g., pending row lock/insert-first), then finalize response atomically; return replay/409 for concurrent duplicate in-flight requests.

2) **High** - Idempotency key is globally scoped, enabling cross-user replay/collision
- Conclusion: **Fail**
- Evidence:
  - Key lookup not scoped by user: `internal/api/middleware/idempotency.go:75`
  - Repository fetches by key alone: `internal/repository/idempotency.go:29`
  - Primary conflict only on key: `internal/repository/idempotency.go:47`
- Impact:
  - Different users reusing same key can observe replay of another user's stored response, risking data leakage and wrong outcomes.
- Minimum actionable fix:
  - Scope idempotency uniqueness and lookup by `(user_id, key)` (or equivalent tenant scope), rejecting cross-user reuse.

3) **High** - Documentation-to-code contradiction on default admin credentials
- Conclusion: **Fail**
- Evidence:
  - README hardcodes password: `README.md:72`, `README.md:145`
  - Code generates random password and writes one-time file: `cmd/server/main.go:223`, `cmd/server/main.go:205`, `cmd/server/main.go:266`
- Impact:
  - Hard-gate verification is undermined; operators following docs can fail initial access and mis-verify security posture.
- Minimum actionable fix:
  - Align README with runtime behavior (or vice versa) and provide deterministic operator retrieval/rotation steps.

### Medium

4) **Medium** - Group-buy failure semantics deviate from prompt (`Failed` vs `expired`) and no explicit resource-slot release mechanism
- Conclusion: **Partial Fail**
- Evidence:
  - Implemented statuses omit `Failed`: `internal/domain/groupbuy.go:14`, `migrations/0002_groupbuy_governance.up.sql:27`
  - Expiry sweep changes status only: `internal/service/groupbuy.go:174`, `internal/repository/groupbuy.go:182`
  - UI claims participant release not backed by explicit resource release logic: `internal/views/groupbuy.templ:124`
- Impact:
  - Requirement traceability is weakened; business semantics may diverge in reporting/operations.
- Minimum actionable fix:
  - Add explicit `failed` terminal state and implement/record explicit release accounting for pre-allocated slots.

5) **Medium** - CSV validation does not implement invalid date rejection from prompt
- Conclusion: **Partial Fail**
- Evidence:
  - Import schema validates only `name,description,capacity`: `internal/service/governance.go:199`, `internal/service/governance.go:242`
  - Only resources CSV import/export endpoints are present: `internal/api/handlers/governance.go:120`, `internal/api/handlers/governance.go:149`
- Impact:
  - Explicit prompt constraint on rejecting invalid dates in bulk validation is not met.
- Minimum actionable fix:
  - Extend CSV domain/model and validator to include required date fields with strict parsing and duplicate/date validation rules.

6) **Medium** - Session cookie is not marked Secure
- Conclusion: **Partial Fail**
- Evidence:
  - Cookie set with `secure=false`: `internal/api/handlers/auth.go:81`
- Impact:
  - In non-local deployments, session token could travel over plaintext HTTP.
- Minimum actionable fix:
  - Set Secure=true when TLS is enabled (environment-aware cookie policy), with SameSite policy explicitly configured.

7) **Medium** - Incremental backup baseline appears tied to last full backup, not last incremental
- Conclusion: **Partial Fail**
- Evidence:
  - `TakeIncremental` uses `LastFull`: `internal/service/backup.go:67`
  - Comment says “previous backup”: `internal/service/backup.go:65`
- Impact:
  - Produces cumulative rather than strictly incremental dumps; can bloat recovery chain and operational cost.
- Minimum actionable fix:
  - Track/use last successful backup checkpoint for incrementals, or clarify contract/documentation and restore application semantics accordingly.

### Low

8) **Low** - Hardcoded analytics anonymization salt in source
- Conclusion: **Partial Fail**
- Evidence:
  - Constant salt wiring in main: `cmd/server/main.go:111`
- Impact:
  - Weaker pseudonymization hygiene and reduced key-rotation flexibility.
- Minimum actionable fix:
  - Move salt to secret config/key material and support rotation strategy.

## 6. Security Review Summary

- Authentication entry points: **Pass**
  - Evidence: register/login/captcha/change-password routes and service policy enforcement `internal/api/router.go:70`, `internal/service/auth.go:73`, `internal/service/auth.go:155`
- Route-level authorization: **Partial Pass**
  - Evidence: `must` middleware on protected groups `internal/api/router.go:93`, `internal/api/router.go:124`, `internal/api/router.go:160`; admin checks in handlers `internal/api/handlers/admin.go:30`
  - Caveat: admin authorization is handler-local rather than centralized; robust but easier to regress if a new handler omits checks.
- Object-level authorization: **Partial Pass**
  - Evidence: booking owner checks `internal/api/handlers/booking.go:138`; document owner checks `internal/api/handlers/document.go:102`, `internal/api/handlers/document.go:124`
  - Caveat: idempotency cross-user replay undermines isolation for write/retry flows.
- Function-level authorization: **Pass**
  - Evidence: admin-only guards for anomalies, deliveries, backups, webhooks, import/export `internal/api/handlers/analytics.go:87`, `internal/api/handlers/notification.go:126`, `internal/api/handlers/governance.go:123`
- Tenant/user data isolation: **Partial Pass**
  - Evidence: user-scoped repos for notifications/todos `internal/repository/notification.go:52`, `internal/repository/notification.go:159`
  - Caveat: global idempotency key namespace creates cross-user collision channel.
- Admin/internal/debug endpoint protection: **Pass**
  - Evidence: explicit admin checks in admin/governance/analytics/notification handlers `internal/api/handlers/admin.go:30`, `internal/api/handlers/governance.go:123`

## 7. Tests and Logging Review

- Unit tests: **Pass**
  - Evidence: password/state/webhook/csv/resource unit suites `unit_tests/password_test.go:11`, `unit_tests/state_test.go:24`, `unit_tests/webhook_test.go:10`, `unit_tests/csv_test.go:10`
- API/integration tests: **Pass (with gaps)**
  - Evidence: broad API suites for auth/booking/groupbuy/document/governance/analytics/notifications/backup `API_tests/auth_test.go:9`, `API_tests/groupbuy_test.go:62`, `API_tests/document_test.go:9`, `API_tests/backup_test.go:10`
  - Gap summary: no test for cross-user idempotency collision or same-key concurrent in-flight behavior.
- Logging categories/observability: **Pass**
  - Evidence: structured JSON logger and request middleware `internal/infrastructure/logger/logger.go:9`, `internal/api/middleware/logger.go:20`
- Sensitive data leakage risk in logs/responses: **Partial Pass**
  - Evidence: no password hash exposure in user JSON model `internal/domain/models.go:39`; request logger includes raw query `internal/api/middleware/logger.go:16`
  - Risk: query-string logging can expose sensitive params if clients send them.

## 8. Test Coverage Assessment (Static Audit)

### 8.1 Test Overview
- Unit tests and API tests exist: yes
  - Evidence: `unit_tests/password_test.go:11`, `API_tests/main_test.go:20`
- Frameworks/entry points:
  - Go `testing` package suites; API tests use `TestMain` health wait `API_tests/main_test.go:20`
- Test commands documented:
  - `./run_tests.sh` and threshold controls documented `README.md:368`, `README.md:388`, `run_tests.sh:1`

### 8.2 Coverage Mapping Table

| Requirement / Risk Point | Mapped Test Case(s) | Key Assertion / Fixture / Mock | Coverage Assessment | Gap | Minimum Test Addition |
|---|---|---|---|---|---|
| Password policy (12+, classes) | `unit_tests/password_test.go:11`, `API_tests/auth_test.go:9` | weak password rejected, detailed validator tests | sufficient | none material | keep regression tests |
| CAPTCHA from 3rd failed attempt + lockout after 5 | `API_tests/auth_test.go:41`, `API_tests/error_branches_test.go:85` | 3rd attempt requires captcha; later returns 423 | basically covered | no inactivity-expiry test | add deterministic session inactivity/expiry test |
| Booking lead-time, overlap, transitions | `API_tests/booking_test.go:10` | 409 on lead-time and overlap; invalid transition conflict | sufficient | no explicit cutoff-window boundary test | add exact 10-minute cutoff boundary test |
| Booking object authorization | `API_tests/booking_test.go:81` | other user gets 403 on GET booking | sufficient | no transition-as-non-owner explicit test | add non-owner transition 403 test |
| Group-buy optimistic oversell prevention | `API_tests/groupbuy_test.go:120` | concurrent joins succeed exactly up to capacity | sufficient | does not test expiry->failed semantics | add deadline expiry state assertion and release accounting test |
| Idempotent replay (same key, same user) | `API_tests/groupbuy_test.go:62` | identical response and `Idempotent-Replay` header | basically covered | no same-key concurrent race coverage | add parallel same-key same-user in-flight test |
| Document supersession + owner access | `API_tests/document_test.go:9`, `API_tests/document_test.go:106` | revision increments, superseded flag/header, 403 for non-owner | sufficient | no related-entity ownership validation test | add test ensuring related booking ownership constraints |
| Governance consent + deletion flow | `API_tests/governance_test.go:34`, `API_tests/governance_test.go:65` | consent lifecycle and hard-delete/anonymize assertions | basically covered | test has fallback manual delete path reducing strict executor verification | add deterministic executor invocation integration test |
| Admin authorization | `API_tests/webhook_test.go:42`, `API_tests/backup_test.go:54`, `API_tests/analytics_test.go:51` | non-admin gets 403 on admin APIs | sufficient | none material | keep |
| Cache TTL bypass admin-only | `API_tests/cache_test.go:27`, `API_tests/cache_test.go:42` | admin gets BYPASS; non-admin cannot bypass | sufficient | no explicit TTL expiry timing test | add short-TTL expiry behavior test |
| Webhook retry/backoff cap 5 | `unit_tests/webhook_test.go:10`, `unit_tests/webhook_test.go:30` | verifies backoff schedule and dead status at cap | sufficient | no network failure e2e retry sequence | add API/integration test stubbing repeated webhook failures |
| Backup full/incremental/list/restore-plan | `API_tests/backup_test.go:10` | creates both kinds, restore plan starts with full | basically covered | no semantic validation of incremental baseline correctness | add test asserting incremental delta baseline between consecutive incrementals |

### 8.3 Security Coverage Audit
- Authentication: **Basically Covered**
  - Evidence: `API_tests/auth_test.go:41`, `API_tests/auth_test.go:108`
  - Residual risk: cookie transport/security attributes not deeply tested.
- Route authorization: **Covered**
  - Evidence: admin 403 tests `API_tests/backup_test.go:54`, `API_tests/webhook_test.go:42`
- Object-level authorization: **Basically Covered**
  - Evidence: booking/doc owner-only tests `API_tests/booking_test.go:81`, `API_tests/document_test.go:120`
  - Residual risk: not all object operations tested across all entities.
- Tenant/data isolation: **Insufficient**
  - Evidence gap: no cross-user idempotency-key collision test; current implementation is globally keyed (`internal/repository/idempotency.go:47`).
- Admin/internal protection: **Covered**
  - Evidence: anomalies/deliveries/backups/webhooks non-admin blocked tests `API_tests/analytics_test.go:57`, `API_tests/notifications_test.go:61`, `API_tests/backup_test.go:64`

### 8.4 Final Coverage Judgment
- **Partial Pass**

Major flows are broadly covered (auth, booking, group-buy, docs, governance, admin gates), but uncovered high-risk cases remain: global idempotency namespace and in-flight same-key concurrency are not tested and could allow severe defects to pass the suite.

## 9. Final Notes
- This audit is static-only and does not claim runtime success.
- The most urgent acceptance risks are:
  1. idempotency semantics (concurrency + cross-user scope),
  2. doc/code contradiction for admin bootstrap,
  3. prompt-fit gaps around group-buy failed-state semantics.
