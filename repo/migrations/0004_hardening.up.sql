-- HarborWorks v5: idempotency at-most-once + per-user scoping, group-buy
-- failed terminal state.
BEGIN;

-- ---------- IDEMPOTENCY: per-user scoping + pending reservation ----------
-- Drop the global PRIMARY KEY on `key` and replace it with a composite key
-- on (user_id, key) so two different users can never collide on the same
-- header value. Anonymous (NULL user_id) requests fall back to a unique
-- index on (key) where user_id IS NULL.
ALTER TABLE idempotency_keys DROP CONSTRAINT IF EXISTS idempotency_keys_pkey;

-- Pending reservation row used by the middleware to atomically claim a key
-- before the handler executes. status = 'pending' means an in-flight request
-- holds the slot; 'completed' means the response has been persisted and is
-- safe to replay.
ALTER TABLE idempotency_keys
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'completed'
        CHECK (status IN ('pending','completed'));

-- Existing rows are old (status_code already set). Make response_body nullable
-- so the pending row can exist before the handler returns.
ALTER TABLE idempotency_keys ALTER COLUMN response_body DROP NOT NULL;
ALTER TABLE idempotency_keys ALTER COLUMN status_code   DROP NOT NULL;

-- Composite uniqueness for authenticated requests.
CREATE UNIQUE INDEX IF NOT EXISTS idem_user_key_uidx
    ON idempotency_keys (user_id, key)
    WHERE user_id IS NOT NULL;

-- Anonymous-request fallback: still globally unique on `key`.
CREATE UNIQUE INDEX IF NOT EXISTS idem_anon_key_uidx
    ON idempotency_keys (key)
    WHERE user_id IS NULL;

-- ---------- GROUP BUYS: failed terminal state ----------
ALTER TABLE group_buys DROP CONSTRAINT IF EXISTS group_buys_status_check;
ALTER TABLE group_buys
    ADD CONSTRAINT group_buys_status_check
        CHECK (status IN ('open','met','expired','canceled','finalized','failed'));

COMMIT;
