# Wacht

[![Go Test](https://github.com/tmater/wacht/actions/workflows/go-test.yml/badge.svg?branch=master)](https://github.com/tmater/wacht/actions/workflows/go-test.yml)
[![Smoke test](https://github.com/tmater/wacht/actions/workflows/smoke.yml/badge.svg?branch=master)](https://github.com/tmater/wacht/actions/workflows/smoke.yml)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-0f172a.svg)](LICENSE)
[![Status: Early development](https://img.shields.io/badge/status-early%20development-b45309.svg)](https://github.com/tmater/wacht)

Distributed uptime monitoring, built in the EU.

Run HTTP, TCP, and DNS checks from multiple probe locations. Quorum-based alerting means you only get paged when a majority of probes agree something is actually down — no false alerts from a single flaky probe.

> **Status:** Early development. Self-hosting works but expect rough edges.

## Quickstart

**Requirements:** Docker, Docker Compose, Git.

```sh
git clone https://github.com/tmater/wacht.git
cd wacht
```

Edit `config/server.yaml` — provision each probe with its own secret and configure your checks:

```yaml
probes:
  - id: probe-1
    secret: replace-with-a-strong-password
  - id: probe-2
    secret: replace-with-a-strong-password
  - id: probe-3
    secret: replace-with-a-strong-password
seed_user:
  email: admin@wacht.local
  password: replace-with-a-strong-password
checks:
  - id: my-site
    type: http
    target: https://example.com
    webhook: https://hooks.example.com/your-webhook-url
  - id: my-db
    type: tcp
    target: db.example.com:5432
```

The code default is to block private and internal targets. The shipped
self-host sample configs set `allow_private_targets: true`, because
monitoring Docker, VPN, and RFC1918 services is a common self-hosted use
case. For hosted or managed-probe deployments, keep that setting disabled on
both the server and the matching probe config.

Edit `config/probe-1.yaml`, `config/probe-2.yaml`, `config/probe-3.yaml` — each probe must use the matching secret provisioned in `config/server.yaml`:

```yaml
secret: replace-with-a-strong-password
server: http://server:8080
probe_id: probe-1
heartbeat_interval: 30s
```

Start everything:

```sh
docker compose up -d
```

The dashboard is available at `http://<your-host>:3000`.

**First login:** open `http://localhost:3000` and sign in with the `seed_user` credentials you configured in `config/server.yaml`. The server refuses to start until the shipped sample secrets and admin password are replaced.

## Check types

| Type   | Target format | Example                | Notes                                         |
|--------|---------------|------------------------|-----------------------------------------------|
| `http` | URL           | `https://example.com`  | Checks for a 2xx response                     |
| `tcp`  | `host:port`   | `db.example.com:5432`  | Checks that a TCP connection can be opened    |
| `dns`  | hostname      | `example.com`          | Checks that the hostname resolves to at least one address |

Private, loopback, and link-local targets are blocked unless
`allow_private_targets: true` is enabled on both the server and the probe.

## How alerting works

A webhook fires when a **strict majority of probes** each report a check as down for **2 consecutive failures**. Recovery requires a non-down majority with **2 consecutive healthy results** from the probes that observed recovery. It fires once on transition (up → down and down → up), deduplicated via an incidents table.

Minimum recommended probe count is 3 — quorum works with 2 but leaves no room for a probe going offline.

Checks run every **30 seconds** per probe.

`/status` marks a probe offline after **90 seconds** without heartbeats by
default. Override that with `probe_offline_after` in `server.yaml` if you want
a shorter or longer UI timeout.

Webhook payload:

```json
{
  "check_id": "my-site",
  "target": "https://example.com",
  "status": "down",
  "probes_down": 2,
  "probes_total": 3
}
```

Recovery notifications use the same payload with `status` set to `up`.

Webhook URLs must be public HTTP(S) endpoints; loopback, private, and
link-local destinations are rejected. Alert delivery is persisted in the
database and retried with backoff in the background so result ingestion is
not blocked by slow destinations. Delivery is timed out after 5 seconds.
If an outage resolves before its `down` alert can be delivered, that stale
opening notification is superseded and the recovery notification becomes the
current delivery target instead. Delivery state is visible in incident
history.

## Status pages

`GET /status` returns the current state of all checks for the authenticated user.
Requests must include a valid session token.

```sh
# Log in and capture the session token:
TOKEN=$(curl -s -X POST http://<your-host>:3000/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@wacht.local","password":"<your-configured-admin-password>"}' | jq -r .token)

# Fetch current status:
curl -H "Authorization: Bearer $TOKEN" http://<your-host>:3000/status
```

Each user also gets one anonymous read-only public page at `/public/{slug}`.
The dashboard exposes that share URL via the Account page, and the backing JSON
endpoint is `GET /api/public/status/{slug}`.

The public page intentionally exposes only check IDs and status state. It does
not include raw targets, webhook URLs, probe details, or incident history.

## Browser tests

Run the browser suite against a disposable packaged stack:

```sh
make browser
```

That boots the normal nginx + server + Postgres path with a dedicated seed
config from `config/server.browser.yaml`, waits for `http://127.0.0.1:13000`,
runs the Playwright specs in `wacht-web/tests/`, then tears the stack down.

Override the default browser stack settings if needed:

```sh
BROWSER_WEB_PORT=14000 BROWSER_PROJECT=my-wacht-browser make browser
```
## License

[AGPL-3.0](LICENSE)
