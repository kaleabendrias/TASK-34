# HarborWorks Issue Resolution Re-Audit (Static-Only)

Date: 2026-04-08
Source baseline: [.tmp/cycle-01/static-audit-harborworks-2026-04-08.md](.tmp/cycle-01/static-audit-harborworks-2026-04-08.md)

## 1. Overall Verdict
- Overall conclusion: **Partial Pass**

Most previously reported issues were addressed with substantial code and schema changes. Two items still have residual gaps in strict requirement-fit/security-hardening terms.

## 2. Scope and Method
- Static-only follow-up review (no runtime execution).
- Compared current implementation against each issue in the baseline report.
- Reviewed updated middleware, repositories, services, migrations, tests, and documentation.

## 3. Issue-by-Issue Resolution Status

### 3.1 High-1: Idempotency unsafe for concurrent same-key requests
- Previous status: Fail
- Current status: **Resolved (with residual risk)**
- Why:
  - Atomic reservation is now attempted before handler execution via Reserve, preventing multiple first-movers for the same key scope.
  - Concurrent in-flight duplicate now returns 409 + Retry-After instead of executing side effects.
  - Completion path stores finalized response and replay path serves persisted response.
- Evidence:
  - [repo/internal/api/middleware/idempotency.go](repo/internal/api/middleware/idempotency.go#L91)
  - [repo/internal/api/middleware/idempotency.go](repo/internal/api/middleware/idempotency.go#L104)
  - [repo/internal/api/middleware/idempotency.go](repo/internal/api/middleware/idempotency.go#L111)
  - [repo/internal/repository/idempotency.go](repo/internal/repository/idempotency.go#L55)
  - [repo/API_tests/idempotency_isolation_test.go](repo/API_tests/idempotency_isolation_test.go#L23)
- Residual risk:
  - If side effects succeed but Complete fails, pending is released and a retry may execute again. This is rarer than the original race, but strict durable at-most-once still depends on successful finalize write.
  - Related line: [repo/internal/api/middleware/idempotency.go](repo/internal/api/middleware/idempotency.go#L139)

### 3.2 High-2: Idempotency key globally scoped (cross-user collision/replay)
- Previous status: Fail
- Current status: **Resolved**
- Why:
  - Idempotency is now scoped by user identity in repository matching and uniqueness strategy.
  - Migration introduces split uniqueness for authenticated and anonymous namespaces.
  - Cross-user isolation test added.
- Evidence:
  - [repo/internal/repository/idempotency.go](repo/internal/repository/idempotency.go#L53)
  - [repo/internal/repository/idempotency.go](repo/internal/repository/idempotency.go#L98)
  - [repo/migrations/0004_hardening.up.sql](repo/migrations/0004_hardening.up.sql#L26)
  - [repo/migrations/0004_hardening.up.sql](repo/migrations/0004_hardening.up.sql#L31)
  - [repo/API_tests/idempotency_isolation_test.go](repo/API_tests/idempotency_isolation_test.go#L104)

### 3.3 High-3: README/code contradiction on admin bootstrap credential
- Previous status: Fail
- Current status: **Resolved**
- Why:
  - README now documents one-time random admin credential file retrieval/rotation instead of hardcoded password.
  - Verification snippets also read password from the one-time file.
- Evidence:
  - [repo/README.md](repo/README.md#L65)
  - [repo/README.md](repo/README.md#L75)
  - [repo/README.md](repo/README.md#L163)
  - [repo/README.md](repo/README.md#L534)

### 3.4 Medium-4: Group-buy failed semantics and explicit slot release
- Previous status: Partial Fail
- Current status: **Resolved**
- Why:
  - Failed terminal state was added to domain and schema constraint.
  - Expiry sweep now distinguishes finalized/failed/expired.
  - Failed path performs atomic status change plus slot release to capacity.
- Evidence:
  - [repo/internal/domain/groupbuy.go](repo/internal/domain/groupbuy.go#L24)
  - [repo/internal/service/groupbuy.go](repo/internal/service/groupbuy.go#L212)
  - [repo/internal/repository/groupbuy.go](repo/internal/repository/groupbuy.go#L185)
  - [repo/internal/repository/groupbuy.go](repo/internal/repository/groupbuy.go#L198)
  - [repo/migrations/0004_hardening.up.sql](repo/migrations/0004_hardening.up.sql#L39)

### 3.5 Medium-5: CSV validation missing invalid-date rejection
- Previous status: Partial Fail
- Current status: **Partially Resolved**
- Why:
  - Strict date parsing logic was added for date-like columns (RFC3339/RFC3339Nano/YYYY-MM-DD).
  - Invalid or empty values in detected date-like columns are now rejected.
- Evidence:
  - [repo/internal/service/governance.go](repo/internal/service/governance.go#L185)
  - [repo/internal/service/governance.go](repo/internal/service/governance.go#L200)
  - [repo/internal/service/governance.go](repo/internal/service/governance.go#L284)
- Remaining gap:
  - The resources import contract still only mandates name/description/capacity. If the original requirement expects a specific mandatory date field, that field is not yet required by schema-level validation.

### 3.6 Medium-6: Session cookie missing Secure flag
- Previous status: Partial Fail
- Current status: **Resolved**
- Why:
  - Secure flag is now configurable and defaults to true.
  - Login, logout, and bad-cookie cleanup all use the configured secure policy.
- Evidence:
  - [repo/internal/service/auth.go](repo/internal/service/auth.go#L43)
  - [repo/internal/api/handlers/auth.go](repo/internal/api/handlers/auth.go#L85)
  - [repo/internal/api/handlers/auth.go](repo/internal/api/handlers/auth.go#L96)
  - [repo/internal/api/middleware/auth.go](repo/internal/api/middleware/auth.go#L40)
  - [repo/docker-compose.yml](repo/docker-compose.yml#L51)

### 3.7 Medium-7: Incremental backup baseline tied to last full
- Previous status: Partial Fail
- Current status: **Resolved**
- Why:
  - Incrementals now use LastSuccessful baseline, matching cumulative-chain expectation for incremental jobs.
- Evidence:
  - [repo/internal/service/backup.go](repo/internal/service/backup.go#L72)
  - [repo/internal/service/backup.go](repo/internal/service/backup.go#L73)
  - [repo/internal/repository/backup.go](repo/internal/repository/backup.go#L22)
  - [repo/internal/repository/backup.go](repo/internal/repository/backup.go#L69)

### 3.8 Low-8: Hardcoded analytics anonymization salt in source
- Previous status: Partial Fail
- Current status: **Partially Resolved**
- Why:
  - Salt is now plumbed through configuration and compose env variables.
- Evidence:
  - [repo/cmd/server/main.go](repo/cmd/server/main.go#L117)
  - [repo/docker-compose.yml](repo/docker-compose.yml#L56)
  - [repo/docker-compose.test.yml](repo/docker-compose.test.yml#L44)
  - [repo/internal/infrastructure/config/config.go](repo/internal/infrastructure/config/config.go#L39)
- Remaining gap:
  - A development fallback literal remains in source config default, so the codebase still contains a hardcoded salt value.
  - Evidence: [repo/internal/infrastructure/config/config.go](repo/internal/infrastructure/config/config.go#L65)

## 4. Re-Audit Summary Table

| Issue | Prior | Current |
|---|---|---|
| Idempotency concurrent same-key race | Fail | Resolved (residual risk) |
| Idempotency cross-user collision | Fail | Resolved |
| Admin credential docs contradiction | Fail | Resolved |
| Group-buy failed semantics + slot release | Partial Fail | Resolved |
| CSV invalid-date validation | Partial Fail | Partially Resolved |
| Session cookie Secure flag | Partial Fail | Resolved |
| Incremental baseline correctness | Partial Fail | Resolved |
| Hardcoded analytics salt | Partial Fail | Partially Resolved |

## 5. Final Judgment
- Resolved: 6/8
- Partially resolved: 2/8
- Unresolved: 0/8

Acceptance posture improves materially versus the baseline audit. Remaining acceptance risk is concentrated in strict idempotency finalization durability and analytics salt hardening defaults, plus potential requirement-fit ambiguity around mandatory CSV date fields.
