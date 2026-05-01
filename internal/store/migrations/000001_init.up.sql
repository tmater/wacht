CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE probes (
    probe_id      TEXT PRIMARY KEY,
    secret_hash   TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',
    version       TEXT NOT NULL DEFAULT '',
    registered_at TIMESTAMPTZ NOT NULL,
    last_seen_at  TIMESTAMPTZ,
    CONSTRAINT probes_status_check CHECK (status IN ('active', 'revoked'))
);

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    public_status_slug TEXT NOT NULL DEFAULT md5(random()::text || clock_timestamp()::text),
    created_at    TIMESTAMPTZ NOT NULL,
    is_admin      BOOLEAN NOT NULL DEFAULT false
);

CREATE UNIQUE INDEX idx_users_public_status_slug
    ON users (public_status_slug);

CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE checks (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    type             TEXT NOT NULL,
    target           TEXT NOT NULL,
    webhook          TEXT NOT NULL DEFAULT '',
    user_id          INTEGER,
    interval_seconds INTEGER NOT NULL DEFAULT 30,
    deleted_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_checks_active_scope_name
    ON checks ((COALESCE(user_id, 0)), name)
    WHERE deleted_at IS NULL;

CREATE TABLE check_probe_state (
    check_id       UUID NOT NULL REFERENCES checks(id),
    probe_id       TEXT NOT NULL,
    last_result_at TIMESTAMPTZ NOT NULL,
    last_outcome   TEXT NOT NULL,
    streak_len     INTEGER NOT NULL,
    expires_at     TIMESTAMPTZ NOT NULL,
    state          TEXT NOT NULL,
    last_error     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (check_id, probe_id),
    CONSTRAINT check_probe_state_last_outcome_check CHECK (last_outcome IN ('', 'up', 'down', 'error')),
    CONSTRAINT check_probe_state_state_check CHECK (state IN ('up', 'down', 'missing', 'error')),
    CONSTRAINT check_probe_state_streak_len_check CHECK (streak_len >= 0)
);

CREATE TABLE incidents (
    id          BIGSERIAL PRIMARY KEY,
    check_id    UUID NOT NULL REFERENCES checks(id),
    user_id     INTEGER,
    started_at  TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ
);

CREATE INDEX idx_incidents_user_started_at ON incidents (user_id, started_at DESC);
CREATE UNIQUE INDEX idx_incidents_open_check ON incidents (check_id) WHERE resolved_at IS NULL;

CREATE TABLE incident_notifications (
    id              BIGSERIAL PRIMARY KEY,
    incident_id     BIGINT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    event           TEXT NOT NULL,
    state           TEXT NOT NULL,
    webhook_url     TEXT NOT NULL,
    payload         JSONB NOT NULL,
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT,
    last_attempt_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    CONSTRAINT incident_notifications_event_check CHECK (event IN ('down', 'up')),
    CONSTRAINT incident_notifications_state_check CHECK (state IN ('pending', 'processing', 'retrying', 'delivered', 'superseded'))
);

CREATE UNIQUE INDEX idx_incident_notifications_incident_event
    ON incident_notifications (incident_id, event);

CREATE INDEX idx_incident_notifications_dispatch
    ON incident_notifications (state, next_attempt_at, id);

CREATE TABLE signup_requests (
    id                     BIGSERIAL PRIMARY KEY,
    email                  TEXT NOT NULL UNIQUE,
    requested_at           TIMESTAMPTZ NOT NULL,
    approved_at            TIMESTAMPTZ,
    rejected_at            TIMESTAMPTZ,
    completed_at           TIMESTAMPTZ,
    user_id                BIGINT REFERENCES users(id),
    setup_token_hash       TEXT,
    setup_token_expires_at TIMESTAMPTZ,
    setup_token_used_at    TIMESTAMPTZ,
    status                 TEXT NOT NULL DEFAULT 'pending',
    CONSTRAINT signup_requests_status_check CHECK (status IN ('pending', 'approved', 'rejected', 'completed'))
);

CREATE UNIQUE INDEX idx_signup_requests_setup_token_hash
    ON signup_requests (setup_token_hash)
    WHERE setup_token_hash IS NOT NULL;
