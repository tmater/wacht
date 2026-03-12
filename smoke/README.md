# Smoke Tests

This directory contains black-box smoke tests for the packaged Wacht stack.

The smoke stack is intentionally realistic:
- Postgres
- Wacht server
- 3 real probes
- the mock target service

Scenarios create and clean up their own checks so they can all run against the
same shared topology.

Current scenarios:
- `startup`
- `crud`
- `quorum`


## Run locally

From the repository root:

```sh
python3 smoke/run.py
```

By default the smoke stack binds the server to `http://localhost:18080` so it
can run next to the normal local dev stack on `8080`. The controllable mock is
bound to `http://localhost:19090`.

Run a single scenario:

```sh
python3 smoke/run.py --scenario startup
```

Reuse an already running stack:

```sh
python3 smoke/run.py --skip-stack --scenario crud
```

Keep the stack running after the smoke run:

```sh
python3 smoke/run.py --keep-up
```

Override the host port if needed:

```sh
SMOKE_HTTP_PORT=28080 python3 smoke/run.py
```

Override the mock control port too:

```sh
SMOKE_HTTP_PORT=28080 SMOKE_MOCK_PORT=29090 python3 smoke/run.py
```
