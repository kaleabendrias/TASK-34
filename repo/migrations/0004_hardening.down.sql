BEGIN;

DROP INDEX IF EXISTS idem_anon_key_uidx;
DROP INDEX IF EXISTS idem_user_key_uidx;
ALTER TABLE idempotency_keys DROP COLUMN IF EXISTS status;
ALTER TABLE idempotency_keys ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (key);

ALTER TABLE group_buys DROP CONSTRAINT IF EXISTS group_buys_status_check;
ALTER TABLE group_buys
    ADD CONSTRAINT group_buys_status_check
        CHECK (status IN ('open','met','expired','canceled','finalized'));

COMMIT;
