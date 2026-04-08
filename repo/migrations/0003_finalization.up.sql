-- HarborWorks v4: hard-delete cascades, group ownership, password rotation,
-- group-buy organizer-link relax (so deleting a user doesn't break the row).
BEGIN;

-- ---------- USERS ----------
-- Forced rotation flag for the seeded admin (and any operator-issued reset).
ALTER TABLE users ADD COLUMN IF NOT EXISTS must_rotate_password BOOLEAN NOT NULL DEFAULT FALSE;

-- ---------- GROUP RESERVATIONS ----------
-- Add organizer_id so we know which authenticated user owns the row.
-- Nullable so seeded reference data and bulk imports can have no owner.
ALTER TABLE group_reservations
    ADD COLUMN IF NOT EXISTS organizer_id UUID REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_group_reservations_organizer ON group_reservations(organizer_id);

-- ---------- HARD-DELETE FK CASCADES ----------
-- The deletion executor needs DELETE FROM users to succeed without manual
-- per-table cleanup. Convert the FKs that currently default to RESTRICT.

-- group_buys.organizer_id: deleting the organiser nulls the field so
-- participants of an in-flight buy don't lose their seat history.
ALTER TABLE group_buys ALTER COLUMN organizer_id DROP NOT NULL;
ALTER TABLE group_buys DROP CONSTRAINT IF EXISTS group_buys_organizer_id_fkey;
ALTER TABLE group_buys
    ADD CONSTRAINT group_buys_organizer_id_fkey
        FOREIGN KEY (organizer_id) REFERENCES users(id) ON DELETE SET NULL;

-- deletion_requests: cascade so the row that triggered the deletion is
-- removed alongside the user (audit lives in the application log).
ALTER TABLE deletion_requests DROP CONSTRAINT IF EXISTS deletion_requests_user_id_fkey;
ALTER TABLE deletion_requests
    ADD CONSTRAINT deletion_requests_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

-- ---------- DATA DICTIONARY UPDATES ----------
INSERT INTO data_dictionary (entity, field, data_type, description, sensitive, tags) VALUES
    ('users','must_rotate_password','bool','Forces a password change on next login', FALSE, ARRAY['policy']),
    ('group_reservations','organizer_id','uuid','Authenticated owner; full PII visible only to owner+admins', FALSE, ARRAY['identity']),
    ('group_reservations','organizer_name','text','PII, masked on shared views', TRUE, ARRAY['pii']),
    ('group_reservations','organizer_email','text','PII, masked on shared views', TRUE, ARRAY['pii'])
ON CONFLICT (entity, field) DO UPDATE SET
    description = EXCLUDED.description,
    sensitive = EXCLUDED.sensitive,
    tags = EXCLUDED.tags;

COMMIT;
