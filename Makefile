DOCKER ?= docker
DEV_COMPOSE = $(DOCKER) compose -f docker-compose.yml -f docker-compose.dev.yml

.PHONY: up down rebuild restart logs test

# Start full dev stack (server + 3 probes + mock)
up:
	$(DEV_COMPOSE) up -d

# Stop and remove containers + volumes (clean slate)
down:
	$(DEV_COMPOSE) down -v

# Rebuild and restart server after Go code changes
rebuild:
	$(DEV_COMPOSE) up -d --build server

# Restart server after config changes (no rebuild)
restart:
	$(DEV_COMPOSE) restart server

# Tail logs from all services
logs:
	$(DEV_COMPOSE) logs -f

# Run tests
test:
	go test ./...
