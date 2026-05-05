---
slug: /probes
---

# Probes

Probes run checks and report results back to the Wacht server. For meaningful
distributed monitoring, run at least three probes.

## Static Probes

Static probes are configured in `server.yaml`:

```yaml
probes:
  - id: probe-1
    secret: replace-with-a-strong-secret-1
  - id: probe-2
    secret: replace-with-a-strong-secret-2
  - id: probe-3
    secret: replace-with-a-strong-secret-3
```

Each probe process needs a matching config:

```yaml
secret: replace-with-a-strong-secret-1
server: http://server:8080
probe_id: probe-1
heartbeat_interval: 30s
```

Static provisioning is useful for Docker Compose and simple self-host setups.

## Dashboard-Created Probes

Admins can create probe credentials from the dashboard.

The server returns the raw secret once. After that, only a hash is stored, so
copy the generated config before leaving the page.

Example generated config:

```yaml
server: https://wacht.example.com
probe_id: probe-api-1
secret: generated-secret
heartbeat_interval: 30s
```

Use the public web origin as `server` when the web container or reverse proxy
forwards `/api` to the Wacht server.

## Running A Probe Elsewhere

A remote probe only needs network access to the Wacht server URL and its own
config file. Use the same image tag as the server.

```sh
docker run --rm \
  -v "$PWD/config/probe-remote.yaml:/etc/wacht/probe.yaml:ro" \
  ghcr.io/tmater/wacht-probe:0.1 --config=/etc/wacht/probe.yaml
```

For production, run probes with a process manager or container restart policy.

## Private Target Policy

The server and each probe have independent `allow_private_targets` settings.

- The server validates check definitions before saving them.
- The probe validates destinations again before it dials.

Private monitoring only works when both sides allow private targets.

## Current Limitations

- Dashboard-created probe credentials cannot be revoked or rotated from the UI
  yet.
- Static probe secrets can be rotated by changing `server.yaml`, changing the
  matching probe config, and restarting the affected services.
- All active probes receive the active check set. Per-probe assignment rules
  are not supported yet.
