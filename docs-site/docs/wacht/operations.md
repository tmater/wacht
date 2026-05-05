---
slug: /operations
---

# Operations

This page covers basic operations for the image-based self-host stack.

## Common Commands

Start:

```sh
docker compose up -d
```

Stop and keep data:

```sh
docker compose down
```

Tail logs:

```sh
docker compose logs -f
```

Restart the server after config changes:

```sh
docker compose restart server
```

Restart one probe after config changes:

```sh
docker compose restart probe-1
```

## Health Checks

Through the web container:

```sh
curl http://127.0.0.1:3000/healthz
```

From inside the Compose network, the server listens on `:8080`.

## Backup

The default database is the `wacht` Postgres database in the `postgres`
container.

Create a SQL backup:

```sh
docker compose exec -T postgres pg_dump -U wacht wacht > wacht-backup.sql
```

For the most consistent backup, stop write traffic first or take a filesystem
snapshot at the infrastructure layer.

## Restore

Restoring replaces data in the target database. Start from a clean database
volume unless you know exactly what you are merging.

```sh
docker compose down -v
docker compose up -d postgres
docker compose exec -T postgres psql -U wacht wacht < wacht-backup.sql
docker compose up -d
```

## Upgrade Images

Use matching image tags across server, probes, and web. To pull newer images
for the configured tags:

```sh
docker compose pull
docker compose up -d
```

Database migrations run when the server starts.

## Change Image Tags

Change the `image:` tags in `compose.yaml` when you intentionally move to a
different release series, then pull and restart:

```sh
docker compose pull
docker compose up -d
```

Read release notes before changing release series.

## Development Checks

When working from the source repository, the development Makefile provides
extra checks:

```sh
make test
make smoke
make browser
```

These are project development checks, not required for a normal image-based
self-host install.

## Troubleshooting

Check container health and logs:

```sh
docker compose ps
docker compose logs server
docker compose logs wacht-web
docker compose logs probe-1
```

Common causes:

- Login fails after first boot: the seed user is only created when no users
  exist. Use the current account password or reset the database volume.
- Checks never run: verify probes authenticate successfully and appear online.
- Private targets are rejected: set `allow_private_targets: true` on both the
  server and the probe.
- Webhooks do not arrive: check incident notification state in the dashboard
  and inspect server logs for delivery errors.
