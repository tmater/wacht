# Wacht

[![Go Test](https://github.com/tmater/wacht/actions/workflows/go-test.yml/badge.svg?branch=master)](https://github.com/tmater/wacht/actions/workflows/go-test.yml)
[![Smoke test](https://github.com/tmater/wacht/actions/workflows/smoke.yml/badge.svg?branch=master)](https://github.com/tmater/wacht/actions/workflows/smoke.yml)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-0f172a.svg)](LICENSE)
[![Status: Early development](https://img.shields.io/badge/status-early%20development-b45309.svg)](https://github.com/tmater/wacht)

Distributed uptime monitoring, built in the EU.

Wacht runs HTTP, TCP, and DNS checks from multiple probes. Quorum-based
alerting means you only get paged when enough probes agree that something is
actually down.

It is intentionally narrow: uptime checks, probe agreement, incident state,
webhook alerts, and a simple status page. It is not a metrics stack or a
general observability platform.

> **Status:** Early development. Self-hosting works, but expect rough edges.

Docs preview: <https://wacht.cloud/>. The docs are work in
progress while the first release is prepared.

## Features

- HTTP, TCP, and DNS checks
- Distributed probe execution
- Quorum-based incident open and recovery logic
- Webhook alerts with durable retry
- Admin-created reusable probe credentials
- Password login, signup approval, and session logout
- One anonymous read-only public status page per user
- Docker Compose self-host setup

## Stack

The default Compose setup starts:

- Postgres for users, checks, incidents, sessions, and current probe state
- `wacht-server` for the HTTP API, auth, monitoring state, and webhook jobs
- three `wacht-probe` containers for local distributed checks
- `wacht-web`, a React UI served through nginx

The web container is the normal browser entrypoint on port `3000`. It serves
the UI and proxies API requests to the server inside the Compose network.

## Quickstart

Requirements for local development: Docker, Docker Compose, and Git.

```sh
git clone https://github.com/tmater/wacht.git
cd wacht
```

Edit `config/server.yaml` and the bundled probe configs before first boot:

- replace the sample probe secrets in `config/server.yaml`
- use the matching secret in each `config/probe-*.yaml`
- set the first `seed_user` email and password
- add or remove initial checks as needed

Start the source-based development stack:

```sh
docker compose up -d --build
```

Open `http://localhost:3000`, sign in with the `seed_user` credentials, and
change the password immediately.

## Check Types

| Type | Target format | Example |
| --- | --- | --- |
| `http` | URL | `https://example.com` |
| `tcp` | `host:port` | `db.example.com:5432` |
| `dns` | hostname | `example.com` |

Checks default to a 30 second interval. The dashboard can create and edit
checks after the first login.

## Self-Host Notes

- The sample configs enable `allow_private_targets: true` so local probes can
  monitor Docker, VPN, LAN, and other RFC1918 targets.
- Disable private targets on both the server and probes if probes should only
  reach public destinations.
- Probe secrets are stored as hashes by the server. Generated probe secrets are
  only shown once.
- Do not expose Postgres publicly.
- `docker compose down -v` removes the database volume.

## Development

Run Go tests:

```sh
make test
```

Run black-box smoke tests against the packaged stack:

```sh
make smoke
```

Run browser tests against the packaged nginx, server, and Postgres path:

```sh
make browser
```

## License

[AGPL-3.0](LICENSE)
