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

Edit `config/server.yaml` — set a strong shared secret and configure your checks:

```yaml
secret: your-secret-here
checks:
  - id: my-site
    type: http
    target: https://example.com
    webhook: https://hooks.example.com/your-webhook-url
  - id: my-db
    type: tcp
    target: db.example.com:5432
```

Edit `config/probe-1.yaml`, `config/probe-2.yaml`, `config/probe-3.yaml` — use the same secret:

```yaml
secret: your-secret-here
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

## How alerting works

A webhook fires when a **strict majority of probes** each report a check as down for **2 consecutive failures**. It fires once on transition (up → down and down → up), deduplicated via an incidents table.

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

Webhook delivery is best-effort — if the endpoint is unreachable, the error is logged and no retry is attempted.

## Status page

`GET /status` returns the current state of all checks. No authentication required.

```sh
curl http://localhost:8080/status
```

## License

[AGPL-3.0](LICENSE)
