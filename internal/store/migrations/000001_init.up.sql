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
    started_at  TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ
);

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
    id           BIGSERIAL PRIMARY KEY,
    email        TEXT NOT NULL UNIQUE,
    requested_at TIMESTAMPTZ NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending'
);
