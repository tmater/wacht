# Smoke Tests

This directory contains black-box smoke tests for the packaged Wacht stack.

The smoke stack is intentionally realistic:
- Postgres
- Wacht server
- 3 real probes
- the mock target service

Scenarios create and clean up their own checks so they can all run against the
same shared topology.


## Run locally

From the repository root:

```sh
make smoke
```

On first run that bootstraps a local virtualenv in `.venv-smoke/` and installs
the smoke-only Python dependency there.

If you want to run pytest directly instead of going through `make`:

```sh
python3 -m venv .venv-smoke
. .venv-smoke/bin/activate
python3 -m pip install -r smoke/requirements.txt
python3 -m pytest smoke/tests -x -s
```

By default the smoke stack binds the server to `http://localhost:18080` so it
can run next to the normal local dev stack on `8080`. The controllable mock is
bound to `http://localhost:19090`.

Run a single scenario:

```sh
python3 -m pytest smoke/tests -x -s -k startup
```

Run the root install path only:

```sh
make release-smoke
```

Override the host port if needed:

```sh
SMOKE_HTTP_PORT=28080 python3 -m pytest smoke/tests -x -s
```

Override the mock control port too:

```sh
SMOKE_HTTP_PORT=28080 SMOKE_MOCK_PORT=29090 python3 -m pytest smoke/tests -x -s
```
