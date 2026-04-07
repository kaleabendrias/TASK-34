-- HarborWorks v3: group-buys, documents, notifications/todos, analytics,
-- governance, webhooks, backups, encrypted notes, admin flag.
BEGIN;

-- ---------- USERS: admin flag + soft anonymisation marker ----------
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE users ADD COLUMN IF NOT EXISTS anonymized_at TIMESTAMPTZ;

-- ---------- BOOKINGS: encrypted notes ----------
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS secure_notes BYTEA;
ALTER TABLE bookings ADD COLUMN IF NOT EXISTS user_anonymized BOOLEAN NOT NULL DEFAULT FALSE;

-- ---------- GROUP BUYS ----------
CREATE TABLE IF NOT EXISTS group_buys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    resource_id     UUID NOT NULL REFERENCES resources(id),
    organizer_id    UUID NOT NULL REFERENCES users(id),
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    threshold       INTEGER NOT NULL DEFAULT 5 CHECK (threshold > 0),
    capacity        INTEGER NOT NULL CHECK (capacity > 0),
    remaining_slots INTEGER NOT NULL CHECK (remaining_slots >= 0),
    starts_at       TIMESTAMPTZ NOT NULL,
    ends_at         TIMESTAMPTZ NOT NULL,
    deadline        TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL DEFAULT 'open'
                    CHECK (status IN ('open','met','expired','canceled','finalized')),
    version         BIGINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_window_gb CHECK (ends_at > starts_at)
);

CREATE INDEX IF NOT EXISTS idx_group_buys_status   ON group_buys(status);
CREATE INDEX IF NOT EXISTS idx_group_buys_deadline ON group_buys(deadline);

CREATE TABLE IF NOT EXISTS group_buy_participants (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    group_buy_id  UUID NOT NULL REFERENCES group_buys(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    quantity      INTEGER NOT NULL DEFAULT 1 CHECK (quantity > 0),
    joined_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (group_buy_id, user_id)
);

-- ---------- IDEMPOTENCY ----------
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key            TEXT PRIMARY KEY,
    user_id        UUID,
    request_hash   TEXT NOT NULL,
    status_code    INTEGER NOT NULL,
    response_body  BYTEA NOT NULL,
    content_type   TEXT NOT NULL DEFAULT 'application/json',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_idem_expires ON idempotency_keys(expires_at);

-- ---------- DOCUMENTS ----------
CREATE TABLE IF NOT EXISTS documents (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    doc_type         TEXT NOT NULL CHECK (doc_type IN ('confirmation_pdf','checkin_pass_png')),
    related_type    TEXT NOT NULL,
    related_id       UUID NOT NULL,
    current_revision INTEGER NOT NULL DEFAULT 1,
    title            TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_documents_owner ON documents(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_documents_related ON documents(related_type, related_id);

CREATE TABLE IF NOT EXISTS document_revisions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id   UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    revision      INTEGER NOT NULL,
    content       BYTEA NOT NULL,
    content_type  TEXT NOT NULL,
    superseded    BOOLEAN NOT NULL DEFAULT FALSE,
    superseded_at TIMESTAMPTZ,
    superseded_by UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (document_id, revision)
);

-- ---------- NOTIFICATIONS & TODOS ----------
CREATE TABLE IF NOT EXISTS notifications (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    title       TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    read_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_unread ON notifications(user_id) WHERE read_at IS NULL;

CREATE TABLE IF NOT EXISTS todos (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    task_type   TEXT NOT NULL,
    title       TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'open'
                CHECK (status IN ('open','in_progress','done','dismissed')),
    due_at      TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_todos_user_status ON todos(user_id, status);

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id UUID REFERENCES notifications(id) ON DELETE SET NULL,
    user_id         UUID,
    channel         TEXT NOT NULL,
    status          TEXT NOT NULL,
    detail          TEXT NOT NULL DEFAULT '',
    delivered_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_at ON notification_deliveries(delivered_at DESC);

-- ---------- ANALYTICS ----------
CREATE TABLE IF NOT EXISTS analytics_events (
    id          BIGSERIAL PRIMARY KEY,
    event_type  TEXT NOT NULL CHECK (event_type IN ('view','favorite','comment','download')),
    target_type TEXT NOT NULL,
    target_id   UUID NOT NULL,
    user_anon   TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_analytics_target  ON analytics_events(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_analytics_created ON analytics_events(created_at);
CREATE INDEX IF NOT EXISTS idx_analytics_etype   ON analytics_events(event_type, created_at);

CREATE TABLE IF NOT EXISTS analytics_hourly (
    bucket_start TIMESTAMPTZ NOT NULL,
    event_type   TEXT NOT NULL,
    target_type  TEXT NOT NULL,
    target_id    UUID NOT NULL,
    count        BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_start, event_type, target_type, target_id)
);

CREATE TABLE IF NOT EXISTS anomaly_alerts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    event_type  TEXT NOT NULL,
    observed    BIGINT NOT NULL,
    baseline    NUMERIC NOT NULL,
    ratio       NUMERIC NOT NULL,
    detail      TEXT NOT NULL DEFAULT ''
);

-- ---------- GOVERNANCE ----------
CREATE TABLE IF NOT EXISTS data_dictionary (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity      TEXT NOT NULL,
    field       TEXT NOT NULL,
    data_type   TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sensitive   BOOLEAN NOT NULL DEFAULT FALSE,
    tags        TEXT[] NOT NULL DEFAULT '{}',
    UNIQUE (entity, field)
);

CREATE TABLE IF NOT EXISTS tags (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS taggings (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tag_id      UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL,
    target_id   UUID NOT NULL,
    UNIQUE (tag_id, target_type, target_id)
);

CREATE TABLE IF NOT EXISTS consent_records (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope        TEXT NOT NULL,
    granted      BOOLEAN NOT NULL,
    version      TEXT NOT NULL DEFAULT 'v1',
    granted_at   TIMESTAMPTZ,
    withdrawn_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_consent_user ON consent_records(user_id);

CREATE TABLE IF NOT EXISTS deletion_requests (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id),
    requested_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    process_after TIMESTAMPTZ NOT NULL,
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','completed','canceled')),
    completed_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_deletion_pending ON deletion_requests(status, process_after);

-- ---------- WEBHOOKS ----------
CREATE TABLE IF NOT EXISTS webhooks (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    target_url    TEXT NOT NULL,
    event_filter  TEXT[] NOT NULL DEFAULT '{}',
    field_mapping JSONB NOT NULL DEFAULT '{}',
    secret        TEXT NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id      UUID NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event_type      TEXT NOT NULL,
    payload         JSONB NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','delivered','failed','dead')),
    last_response   TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_pending
    ON webhook_deliveries(status, next_attempt_at);

-- ---------- BACKUPS ----------
CREATE TABLE IF NOT EXISTS backups (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind       TEXT NOT NULL CHECK (kind IN ('full','incremental')),
    path       TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    taken_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    detail     TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_backups_taken ON backups(taken_at DESC);

-- ---------- DATA DICTIONARY: seed core entities ----------
INSERT INTO data_dictionary (entity, field, data_type, description, sensitive, tags) VALUES
    ('users','id','uuid','Internal user id', FALSE, ARRAY['identity']),
    ('users','username','text','Login handle, masked on shared views', TRUE, ARRAY['pii','identity']),
    ('users','password_hash','text','bcrypt hash, never exposed', TRUE, ARRAY['secret']),
    ('users','is_blacklisted','bool','Hard block flag', FALSE, ARRAY['policy']),
    ('bookings','user_id','uuid','Owner of the booking', FALSE, ARRAY['identity']),
    ('bookings','start_time','timestamptz','Start of the reservation window', FALSE, ARRAY['schedule']),
    ('bookings','end_time','timestamptz','End of the reservation window', FALSE, ARRAY['schedule']),
    ('bookings','notes','text','Plain-text note (non-sensitive)', FALSE, ARRAY['content']),
    ('bookings','secure_notes','bytea','AES-GCM encrypted note', TRUE, ARRAY['secret','pii']),
    ('group_buys','threshold','int','Participants required to finalize', FALSE, ARRAY['policy']),
    ('group_buys','remaining_slots','int','Optimistic-locked counter', FALSE, ARRAY['policy']),
    ('analytics_events','user_anon','text','Hashed anonymous identifier', FALSE, ARRAY['analytics'])
ON CONFLICT (entity, field) DO NOTHING;

INSERT INTO tags (name, description) VALUES
    ('pii',       'Personally identifiable information'),
    ('secret',    'Sensitive content stored encrypted or never exposed'),
    ('identity',  'Identity-related field'),
    ('policy',    'Policy / governance flag'),
    ('schedule',  'Scheduling field'),
    ('content',   'User-supplied content'),
    ('analytics', 'Analytics-only field')
ON CONFLICT (name) DO NOTHING;

COMMIT;
