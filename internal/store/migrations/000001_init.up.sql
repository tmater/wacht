CREATE TABLE check_results (
    id         BIGSERIAL PRIMARY KEY,
    check_id   TEXT NOT NULL,
    probe_id   TEXT NOT NULL,
    type       TEXT NOT NULL,
    target     TEXT NOT NULL,
    up         BOOLEAN NOT NULL,
    latency_ms INTEGER NOT NULL,
    error      TEXT,
    timestamp  TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_check_results_timestamp ON check_results (timestamp);

CREATE TABLE probes (
    probe_id      TEXT PRIMARY KEY,
    secret_hash   TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'active',
    version       TEXT NOT NULL DEFAULT '',
    registered_at TIMESTAMPTZ NOT NULL,
    last_seen_at  TIMESTAMPTZ,
    CONSTRAINT probes_status_check CHECK (status IN ('active', 'revoked'))
);

CREATE TABLE incidents (
    id          BIGSERIAL PRIMARY KEY,
    check_id    TEXT NOT NULL,
    user_id     INTEGER,
    started_at  TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ
);

CREATE INDEX idx_incidents_user_started_at ON incidents (user_id, started_at DESC);
CREATE UNIQUE INDEX idx_incidents_open_check ON incidents (check_id) WHERE resolved_at IS NULL;

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL,
    is_admin      BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE sessions (
    token      TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE checks (
    id               TEXT PRIMARY KEY,
    type             TEXT NOT NULL,
    target           TEXT NOT NULL,
    webhook          TEXT NOT NULL DEFAULT '',
    user_id          INTEGER,
    interval_seconds INTEGER NOT NULL DEFAULT 30
);

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
