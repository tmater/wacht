---
slug: /config-reference
---

# Config Reference

Wacht has one server config and one config per probe. The example Compose file
renders these configs inside the containers from `.env` and mounts database
credentials from `secrets/`. If you run the binaries directly or provide your
own Compose file, use the same shapes below.

Durations should be written as Go-style duration strings such as `30s`, `1m`,
or `2h`.

## Runtime Secrets

The server needs a Postgres DSN at startup. Resolution order:

1. `WACHT_DATABASE_DSN`
2. `WACHT_DATABASE_DSN_FILE`

The Compose examples use `WACHT_DATABASE_DSN_FILE=/run/secrets/wacht_database_dsn`.

## Server Config

Default path inside the example server container:

```text
/tmp/server.yaml
```

| Key | Default | Description |
| --- | --- | --- |
| `allow_private_targets` | `false` | Allows private targets. |
| `probes` | `[]` | Static probe credentials. Each entry needs `id` and `secret`. |
| `seed_user` | empty | First admin account, created only when no users exist. |
| `checks` | `[]` | Initial checks inserted on startup if they do not already exist. |
| `auth_rate_limit.requests` | `10` | Auth request limit per client bucket. |
| `auth_rate_limit.window` | `1m` | Rate-limit window. |
| `trusted_proxies` | loopback CIDRs | CIDRs trusted for forwarded client IP headers. |
| `probe_offline_after` | `90s` | Heartbeat age after which a probe is offline. |

Example:

```yaml
allow_private_targets: true
trusted_proxies:
  - 127.0.0.1/8
  - ::1/128

probes:
  - id: probe-1
    secret: replace-with-a-strong-secret

seed_user:
  email: admin@example.com
  password: replace-with-a-strong-password

checks:
  - name: website
    type: http
    target: https://example.com
    webhook: https://hooks.example.com/wacht
    interval: 30
```

## Probe Config

Default path inside the example probe containers:

```text
/tmp/probe.yaml
```

| Key | Default | Description |
| --- | --- | --- |
| `secret` | required | Probe secret matching the server-side credential. |
| `server` | required | Base URL for the Wacht server or web proxy. |
| `probe_id` | required | Probe ID matching the server-side credential. |
| `heartbeat_interval` | `30s` | Probe liveness and check refresh interval. |
| `result_flush_interval` | `10s` | Queued result flush interval. |
| `allow_private_targets` | `false` | Allows private targets. |

Example:

```yaml
allow_private_targets: true
secret: replace-with-a-strong-secret
server: https://wacht.example.com
probe_id: probe-1
heartbeat_interval: 30s
result_flush_interval: 10s
```

## Checks

Checks can be seeded in `server.yaml` or created from the dashboard.

| Field | Default | Description |
| --- | --- | --- |
| `name` | required | User-facing check name. Names are unique per user while active. |
| `type` | `http` | One of `http`, `tcp`, or `dns`. |
| `target` | required | Target to check. Format depends on type. |
| `webhook` | empty | Optional webhook URL for down and recovery alerts. |
| `interval` | `30` | Check interval in seconds. Must be from `1` to `86400`. |

Target formats:

| Type | Format | Example |
| --- | --- | --- |
| `http` | URL | `https://example.com` |
| `tcp` | `host:port` | `db.example.com:5432` |
| `dns` | hostname | `example.com` |

Private targets must be allowed on both the server and the probe. The server
validates check definitions; the probe validates the destination again before
dialing.
