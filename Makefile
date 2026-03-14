DOCKER ?= docker
DEV_COMPOSE = $(DOCKER) compose -f docker-compose.yml -f docker-compose.dev.yml
PYTHON ?= python3
SMOKE_VENV ?= .venv-smoke
SMOKE_PYTHON = $(SMOKE_VENV)/bin/python3
SMOKE_STAMP = $(SMOKE_VENV)/.requirements-installed

.PHONY: up down rebuild restart logs test smoke smoke-venv

# Start full dev stack (server + 3 probes + mock)
up:
	$(DEV_COMPOSE) up -d --build

# Stop and remove containers + volumes (clean slate)
down:
	$(DEV_COMPOSE) down -v

# Rebuild everything from scratch (wipes DB volume)
rebuild: test
	$(DEV_COMPOSE) down -v
	$(DEV_COMPOSE) up -d --build

# Restart server after config changes (no rebuild)
restart:
	$(DEV_COMPOSE) restart server

# Tail logs from all services
logs:
	$(DEV_COMPOSE) logs -f

# Run tests
test:
	go test ./...

# Bootstrap the isolated virtualenv used by smoke tests.
$(SMOKE_PYTHON):
	$(PYTHON) -m venv $(SMOKE_VENV)

$(SMOKE_STAMP): smoke/requirements.txt | $(SMOKE_PYTHON)
	$(SMOKE_PYTHON) -m pip install -r smoke/requirements.txt
	touch $(SMOKE_STAMP)

smoke-venv: $(SMOKE_STAMP)

# Run black-box smoke tests against the packaged stack
smoke: $(SMOKE_STAMP)
	$(SMOKE_PYTHON) -m pytest smoke/tests -x -s
