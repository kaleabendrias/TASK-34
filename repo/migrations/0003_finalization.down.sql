BEGIN;
ALTER TABLE deletion_requests DROP CONSTRAINT IF EXISTS deletion_requests_user_id_fkey;
ALTER TABLE deletion_requests
    ADD CONSTRAINT deletion_requests_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE group_buys DROP CONSTRAINT IF EXISTS group_buys_organizer_id_fkey;
ALTER TABLE group_buys
    ADD CONSTRAINT group_buys_organizer_id_fkey
        FOREIGN KEY (organizer_id) REFERENCES users(id);
ALTER TABLE group_buys ALTER COLUMN organizer_id SET NOT NULL;

DROP INDEX IF EXISTS idx_group_reservations_organizer;
ALTER TABLE group_reservations DROP COLUMN IF EXISTS organizer_id;

ALTER TABLE users DROP COLUMN IF EXISTS must_rotate_password;
COMMIT;
