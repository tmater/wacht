# Wacht

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
    secret: replace-with-a-strong-secret-1
  - id: probe-2
    secret: replace-with-a-strong-secret-2
  - id: probe-3
    secret: replace-with-a-strong-secret-3
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
secret: replace-with-a-strong-secret-1
server: http://server:8080
probe_id: probe-1
heartbeat_interval: 30s
```

Start everything:

```sh
docker compose up -d
```

The server listens on port `8080`. The dashboard is available at `http://localhost:3000`.

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

Webhook delivery is best-effort. Webhook URLs must be public HTTP(S)
endpoints; loopback, private, and link-local destinations are rejected.
Alerts are queued for background delivery so result ingestion is not
blocked by slow destinations. Delivery is timed out after 5 seconds, and
no retry is attempted.

## Status page

`GET /status` returns the current state of all checks for the authenticated user.

```sh
curl http://localhost:8080/status
```

## License

[AGPL-3.0](LICENSE)
