REPO_ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
DOCKER ?= docker
DEV_COMPOSE = $(DOCKER) compose -f docker-compose.yml -f docker-compose.dev.yml
BROWSER_PROJECT ?= wacht-browser
BROWSER_WEB_PORT ?= 13000
BROWSER_SERVER_CONFIG ?= $(REPO_ROOT)/config/server.browser.yaml
BROWSER_EMAIL ?= browser@wacht.local
BROWSER_PASSWORD ?= browserpassword
BROWSER_SERVICES = postgres server wacht-web
BROWSER_WEB_DIR = $(REPO_ROOT)/wacht-web
BROWSER_COMPOSE = COMPOSE_PROJECT_NAME=$(BROWSER_PROJECT) SERVER_CONFIG_PATH=$(BROWSER_SERVER_CONFIG) WACHT_WEB_PORT=$(BROWSER_WEB_PORT) $(DOCKER) compose -f $(REPO_ROOT)/docker-compose.yml
PYTHON ?= python3
SMOKE_VENV ?= .venv-smoke
SMOKE_PYTHON = $(SMOKE_VENV)/bin/python3
SMOKE_STAMP = $(SMOKE_VENV)/.requirements-installed

.PHONY: up down rebuild restart logs test smoke release-smoke smoke-venv browser browser-up browser-down browser-logs

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

# Start the packaged browser-test stack with a deterministic seed user.
browser-up:
	$(BROWSER_COMPOSE) up -d --build $(BROWSER_SERVICES)

# Stop and remove the browser-test stack.
browser-down:
	$(BROWSER_COMPOSE) down -v

# Tail logs from the browser-test stack.
browser-logs:
	$(BROWSER_COMPOSE) logs -f

# Run browser tests against the packaged nginx+server stack.
browser:
	(cd $(BROWSER_WEB_DIR) && npm ci)
	(cd $(BROWSER_WEB_DIR) && npx playwright install chromium)
	@set -eu; \
	trap '$(BROWSER_COMPOSE) down -v' EXIT INT TERM; \
	$(BROWSER_COMPOSE) up -d --build $(BROWSER_SERVICES); \
	for i in $$(seq 1 60); do \
		if curl -fsS http://127.0.0.1:$(BROWSER_WEB_PORT)/healthz >/dev/null; then \
			break; \
		fi; \
		if [ $$i -eq 60 ]; then \
			echo "browser stack did not become ready on http://127.0.0.1:$(BROWSER_WEB_PORT)" >&2; \
			exit 1; \
		fi; \
		sleep 1; \
	done; \
	(cd $(BROWSER_WEB_DIR) && PLAYWRIGHT_BASE_URL=http://127.0.0.1:$(BROWSER_WEB_PORT) E2E_EMAIL=$(BROWSER_EMAIL) E2E_PASSWORD=$(BROWSER_PASSWORD) npm run test:e2e)

# Run the root docker-compose.yml release-install smoke path.
release-smoke: $(SMOKE_STAMP)
	$(SMOKE_PYTHON) -m pytest smoke/release/test_install.py -x -s
