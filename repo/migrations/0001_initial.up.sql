-- HarborWorks initial schema (v2: auth, sessions, resources, state machine)
BEGIN;

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ---------- USERS ----------
CREATE TABLE IF NOT EXISTS users (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username           TEXT NOT NULL UNIQUE,
    password_hash      TEXT NOT NULL,
    is_blacklisted     BOOLEAN NOT NULL DEFAULT FALSE,
    blacklist_reason   TEXT NOT NULL DEFAULT '',
    failed_attempts    INTEGER NOT NULL DEFAULT 0,
    locked_until       TIMESTAMPTZ,
    last_login_at      TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT username_format CHECK (char_length(username) BETWEEN 3 AND 64)
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users(LOWER(username));

-- ---------- SESSIONS ----------
CREATE TABLE IF NOT EXISTS sessions (
    id               TEXT PRIMARY KEY,
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ NOT NULL,
    user_agent       TEXT NOT NULL DEFAULT '',
    ip               TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

-- ---------- CAPTCHA CHALLENGES ----------
CREATE TABLE IF NOT EXISTS captcha_challenges (
    token       TEXT PRIMARY KEY,
    question    TEXT NOT NULL,
    answer      TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed    BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_captcha_expires ON captcha_challenges(expires_at);

-- ---------- RESOURCES ----------
CREATE TABLE IF NOT EXISTS resources (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    capacity    INTEGER NOT NULL DEFAULT 1 CHECK (capacity > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ---------- GROUP RESERVATIONS ----------
CREATE TABLE IF NOT EXISTS group_reservations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    organizer_name  TEXT NOT NULL DEFAULT '',
    organizer_email TEXT NOT NULL,
    capacity        INTEGER NOT NULL CHECK (capacity > 0),
    notes           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_groups_created_at ON group_reservations(created_at DESC);

-- ---------- BOOKINGS ----------
CREATE TABLE IF NOT EXISTS bookings (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    resource_id  UUID NOT NULL REFERENCES resources(id) ON DELETE RESTRICT,
    group_id     UUID REFERENCES group_reservations(id) ON DELETE SET NULL,
    party_size   INTEGER NOT NULL DEFAULT 1 CHECK (party_size > 0),
    start_time   TIMESTAMPTZ NOT NULL,
    end_time     TIMESTAMPTZ NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending_confirmation'
                 CHECK (status IN ('pending_confirmation','waitlisted','checked_in','completed','canceled')),
    notes        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_window CHECK (end_time > start_time)
);

CREATE INDEX IF NOT EXISTS idx_bookings_user_id     ON bookings(user_id);
CREATE INDEX IF NOT EXISTS idx_bookings_resource_id ON bookings(resource_id);
CREATE INDEX IF NOT EXISTS idx_bookings_group_id    ON bookings(group_id);
CREATE INDEX IF NOT EXISTS idx_bookings_status      ON bookings(status);
CREATE INDEX IF NOT EXISTS idx_bookings_start_time  ON bookings(start_time);
CREATE INDEX IF NOT EXISTS idx_bookings_user_start  ON bookings(user_id, start_time);

COMMIT;
