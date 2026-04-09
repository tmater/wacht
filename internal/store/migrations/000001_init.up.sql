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
    uid              BIGSERIAL PRIMARY KEY,
    id               TEXT NOT NULL,
    type             TEXT NOT NULL,
    target           TEXT NOT NULL,
    webhook          TEXT NOT NULL DEFAULT '',
    user_id          INTEGER,
    interval_seconds INTEGER NOT NULL DEFAULT 30,
    deleted_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_checks_active_id
    ON checks (id)
    WHERE deleted_at IS NULL;

CREATE TABLE monitoring_journal (
    id          BIGSERIAL PRIMARY KEY,
    kind        TEXT NOT NULL,
    check_id    TEXT,
    probe_id    TEXT,
    message     TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ,
    occurred_at TIMESTAMPTZ NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE incidents (
    id          BIGSERIAL PRIMARY KEY,
    check_uid   BIGINT NOT NULL REFERENCES checks(uid),
    user_id     INTEGER,
    started_at  TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ
);

CREATE INDEX idx_incidents_user_started_at ON incidents (user_id, started_at DESC);
CREATE UNIQUE INDEX idx_incidents_open_check ON incidents (check_uid) WHERE resolved_at IS NULL;

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
