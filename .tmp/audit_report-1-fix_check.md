# HarborWorks Issue Resolution Re-Audit (Static-Only)

Date: 2026-04-09
Baseline: [.tmp/audit_report-1.md](.tmp/audit_report-1.md)

## 1. Overall Verdict
- Overall conclusion: **Pass**

All eight findings from the baseline audit now have concrete implementation-level fixes. No baseline issue remains open in this static re-check.

## 2. Scope and Method
- Static-only follow-up review (no runtime execution performed in this pass).
- Re-checked code paths and tests tied to each baseline finding.
- Focused on middleware, repository, service, migrations, config, compose, and test updates.

## 3. Issue-by-Issue Resolution Status

### 3.1 High-1: Idempotency unsafe for concurrent same-key requests
- Previous status: Fail
- Current status: **Fixed**
- Why:
  - Request key is now reserved atomically before handler execution.
  - In-flight duplicate requests receive 409 with Retry-After instead of re-running side effects.
  - Completed records are replayed deterministically.
  - Durable-failure behavior is explicitly hardened: if finalize write fails, pending reservation is intentionally retained to preserve at-most-once behavior.
- Evidence:
  - [repo/internal/api/middleware/idempotency.go](repo/internal/api/middleware/idempotency.go#L86)
  - [repo/internal/api/middleware/idempotency.go](repo/internal/api/middleware/idempotency.go#L104)
  - [repo/internal/api/middleware/idempotency.go](repo/internal/api/middleware/idempotency.go#L121)
  - [repo/internal/api/middleware/idempotency.go](repo/internal/api/middleware/idempotency.go#L157)
  - [repo/internal/repository/idempotency.go](repo/internal/repository/idempotency.go#L53)
  - [repo/API_tests/idempotency_durable_test.go](repo/API_tests/idempotency_durable_test.go#L10)

### 3.2 High-2: Idempotency key globally scoped (cross-user collision/replay)
- Previous status: Fail
- Current status: **Fixed**
- Why:
  - Idempotency reservation/lookup/update/delete are all scoped by user identity (or anonymous null scope).
  - This prevents cross-user key collision and replay leakage.
- Evidence:
  - [repo/internal/api/middleware/idempotency.go](repo/internal/api/middleware/idempotency.go#L80)
  - [repo/internal/repository/idempotency.go](repo/internal/repository/idempotency.go#L50)
  - [repo/internal/repository/idempotency.go](repo/internal/repository/idempotency.go#L95)

### 3.3 High-3: README/code contradiction on admin bootstrap credential
- Previous status: Fail
- Current status: **Fixed**
- Why:
  - Documentation now reflects one-time bootstrap credential retrieval from file instead of claiming a fixed default password.
- Evidence:
  - [repo/README.md](repo/README.md#L161)

### 3.4 Medium-4: Group-buy failed semantics and explicit slot release
- Previous status: Partial Fail
- Current status: **Fixed**
- Why:
  - `failed` terminal status exists in domain model.
  - Expiry sweep now routes not-met campaigns to failed path.
  - Failed transition performs slot release behavior explicitly.
- Evidence:
  - [repo/internal/domain/groupbuy.go](repo/internal/domain/groupbuy.go#L24)
  - [repo/internal/service/groupbuy.go](repo/internal/service/groupbuy.go#L200)
  - [repo/internal/service/groupbuy.go](repo/internal/service/groupbuy.go#L217)

### 3.5 Medium-5: CSV validation missing invalid-date rejection
- Previous status: Partial Fail
- Current status: **Fixed**
- Why:
  - Import validation now enforces mandatory date-column presence (`effective_date` or `created_at`).
  - Date-like columns are strictly validated as RFC3339/RFC3339Nano or YYYY-MM-DD.
  - Unit tests cover missing mandatory date column and invalid date values.
- Evidence:
  - [repo/internal/service/governance.go](repo/internal/service/governance.go#L234)
  - [repo/internal/service/governance.go](repo/internal/service/governance.go#L276)
  - [repo/unit_tests/csv_test.go](repo/unit_tests/csv_test.go#L76)
  - [repo/unit_tests/csv_test.go](repo/unit_tests/csv_test.go#L88)

### 3.6 Medium-6: Session cookie missing Secure flag
- Previous status: Partial Fail
- Current status: **Fixed**
- Why:
  - Session cookie `Secure` is now controlled by config and used consistently in auth cookie set/clear paths.
  - Production compose defaults keep secure cookies enabled.
- Evidence:
  - [repo/internal/api/handlers/auth.go](repo/internal/api/handlers/auth.go#L85)
  - [repo/internal/api/handlers/auth.go](repo/internal/api/handlers/auth.go#L96)
  - [repo/docker-compose.yml](repo/docker-compose.yml#L51)

### 3.7 Medium-7: Incremental backup baseline tied to last full
- Previous status: Partial Fail
- Current status: **Fixed**
- Why:
  - Incremental backup now uses last successful backup checkpoint, not only last full.
- Evidence:
  - [repo/internal/service/backup.go](repo/internal/service/backup.go#L73)

### 3.8 Low-8: Hardcoded analytics anonymization salt in source
- Previous status: Partial Fail
- Current status: **Fixed**
- Why:
  - Config now requires `ANALYTICS_ANON_SALT` with no in-code fallback default.
  - Compose/test compose explicitly provide environment values.
- Evidence:
  - [repo/internal/infrastructure/config/config.go](repo/internal/infrastructure/config/config.go#L13)
  - [repo/internal/infrastructure/config/config.go](repo/internal/infrastructure/config/config.go#L75)
  - [repo/docker-compose.yml](repo/docker-compose.yml#L56)
  - [repo/docker-compose.test.yml](repo/docker-compose.test.yml#L44)

## 4. Re-Audit Summary Table

| Issue | Prior | Current |
|---|---|---|
| Idempotency concurrent same-key race | Fail | Fixed |
| Idempotency cross-user collision | Fail | Fixed |
| Admin credential docs contradiction | Fail | Fixed |
| Group-buy failed semantics + slot release | Partial Fail | Fixed |
| CSV invalid-date validation | Partial Fail | Fixed |
| Session cookie Secure flag | Partial Fail | Fixed |
| Incremental baseline correctness | Partial Fail | Fixed |
| Hardcoded analytics salt | Partial Fail | Fixed |

## 5. Final Judgment
- Fixed: 8/8
- Partially fixed: 0/8
- Unresolved: 0/8

The baseline acceptance blockers and partial passes have now been fully addressed in static code review terms.
