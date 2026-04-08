# HarborWorks Questions and Clarifications

## 1. Authoritative runtime path for full verification
Question: Should full-system verification be performed natively or via containers?

My Understanding: Containerized verification should be the authoritative acceptance path, with native checks optional for local iteration.

Solution: Adopted Docker-based verification as primary; retained local/native checks as secondary developer workflow.

## 2. Session model under offline intranet constraints
Question: Should sessions be stateless JWTs or server-managed sessions for offline-only deployment?

My Understanding: Server-managed local sessions better support inactivity timeout, lockout handling, and admin revocation with predictable control.

Solution: Implemented cookie-based server sessions with inactivity expiry policy enforcement and local credential auth only.

## 3. Authorization enforcement location
Question: Are frontend navigation checks sufficient for authorization, or must API enforce all role and ownership rules?

My Understanding: Frontend checks improve UX, but API-side authorization is mandatory and authoritative.

Solution: Enforced authz gates in API middleware/handlers, including admin-only route protection and object ownership checks.

## 4. Booking overlap semantics
Question: Should overlap prevention apply only per resource or across all resources for the same user and time window?

My Understanding: The requirement implies user-level duplicate prevention regardless of resource.

Solution: Implemented user-level overlap validation across booking windows to prevent concurrent double-booking.

## 5. Group-buy default policy values
Question: Which defaults apply when threshold and deadline are omitted by caller?

My Understanding: Prompt defines threshold default as 5 and deadline default as 24 hours.

Solution: Applied defaults of threshold=5 and deadline=24h unless explicitly provided.

## 6. Duplicate click behavior on join actions
Question: What should happen when the same action is submitted repeatedly due to UI retries/double clicks?

My Understanding: Repeated submissions for the same client action must return one stable outcome without duplicated side effects.

Solution: Used idempotency keys on write actions and persisted idempotency records to enforce at-most-once effects.

## 7. Waitlist capacity accounting
Question: Do waitlisted bookings consume active capacity before promotion?

My Understanding: Waitlisted entries should represent queued intent and should not consume active seats until promoted.

Solution: Excluded waitlisted entries from active seat-consumption calculations while preserving workflow state tracking.

## 8. Document revision retrieval behavior
Question: Should superseded revisions remain downloadable or be blocked once a newer revision exists?

My Understanding: Prompt asks for visible history and superseded marking, not deletion of prior revisions.

Solution: Kept prior revisions retrievable, marked superseded in metadata and response indicators.

## 9. Deletion and analytics retention boundary
Question: After self-service deletion, what data can remain while meeting privacy requirements?

My Understanding: Personal data must be hard-deleted, while analytics may remain only in anonymized, non-identifiable form.

Solution: Implemented hard deletion for personal records and anonymization/detachment for retained analytics.

## 10. Webhook network scope in offline mode
Question: Should outbound webhook targets allow public internet hosts?

My Understanding: Offline-only integration implies callbacks should remain on local/private network scope.

Solution: Restricted webhook target validation to local/private host ranges and applied bounded exponential retry policy.
