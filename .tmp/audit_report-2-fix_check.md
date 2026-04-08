# Cycle-02 Fix Verification Report (Static Review)

Date: 2026-04-08  
Scope: Verify whether issues from the prior audit report were fixed, using static code/document review only.

## Overall Result
- **All previously reported issues appear fixed statically.**
- Runtime behavior is still **Manual Verification Required** because this check did not execute the app/tests.

## Issue-by-Issue Verification

### 1) High: Session inactivity semantics (sliding server session vs non-sliding cookie)
- Previous status: Open
- Current status: **Fixed (static evidence)**
- Evidence:
  - Cookie is now reissued on each authenticated request in middleware: `internal/api/middleware/auth.go:63`
  - Uses inactivity window for cookie Max-Age: `internal/api/middleware/auth.go:62`
  - Session sliding remains server-side via `Touch(...)`: `internal/service/auth.go:233`
  - New API test added for sliding cookie behavior: `API_tests/sliding_and_organizer_test.go:19`
- Note: Runtime confirmation still requires executing test suite.

### 2) High: Group-buy organizer nullability mismatch (schema NULL vs model non-NULL)
- Previous status: Open
- Current status: **Fixed (static evidence)**
- Evidence:
  - Domain model changed to nullable organizer pointer: `internal/domain/groupbuy.go:47`
  - Repository now scans nullable UUID and maps safely: `internal/repository/groupbuy.go:275`
  - Create path writes nullable organizer UUID safely: `internal/repository/groupbuy.go:65`
  - Service handles nil organizer when sending notifications: `internal/service/groupbuy.go:229`
  - Regression test added for read-after-organizer-delete: `API_tests/sliding_and_organizer_test.go:54`

### 3) Medium: Waitlisted bookings counted as active seat consumption
- Previous status: Open
- Current status: **Fixed (static evidence)**
- Evidence:
  - Resource overlap seat sum excludes waitlisted status now: `internal/repository/resource.go:129`
  - Slot-capacity computation explicitly skips waitlisted entries: `internal/service/resource.go:163`

### 4) Medium: UI labels were technical enum strings instead of user-friendly labels
- Previous status: Open
- Current status: **Fixed (static evidence)**
- Evidence:
  - New centralized label helper: `internal/views/labels.go:14`
  - Booking page now renders human labels: `internal/views/booking.templ:58`
  - Group-buy page now renders human labels: `internal/views/groupbuy.templ:94`
  - Availability and group detail pages updated similarly: `internal/views/availability.templ:98`, `internal/views/group.templ:74`
  - Notification todo status labels updated: `internal/views/notification.templ:78`

### 5) Medium: README migration section stale/inconsistent
- Previous status: Open
- Current status: **Fixed (static evidence)**
- Evidence:
  - Architecture section now lists all migrations through `0004_hardening`: `README.md:454`, `README.md:456`
  - Repository tree section updated similarly: `README.md:697`, `README.md:698`

### 6) Low: Admin authorization should be centralized at route group middleware
- Previous status: Open
- Current status: **Fixed (static evidence)**
- Evidence:
  - New middleware gate introduced: `internal/api/middleware/auth.go:83`
  - `/api/admin` group now includes `requireAdmin`: `internal/api/router.go:164`
  - Per-handler duplicated checks removed and comments updated: `internal/api/handlers/admin.go:18`, `internal/api/handlers/analytics.go:85`, `internal/api/handlers/governance.go:121`, `internal/api/handlers/notification.go:124`

## Final Conclusion
- **Static conclusion:** all previously listed issues are addressed in code/docs.
- **Execution conclusion:** cannot be proven without running tests/app; manual verification is required for runtime assurance.

## Manual Verification Checklist (recommended)
1. Run the API test that validates sliding cookie behavior: `API_tests/sliding_and_organizer_test.go:19`.
2. Run the API test that validates organizer NULL read-path behavior: `API_tests/sliding_and_organizer_test.go:54`.
3. Confirm admin-only route behavior still passes all authorization tests.
4. Spot-check UI labels on booking/group/group-buy/notification pages for human-readable wording.
