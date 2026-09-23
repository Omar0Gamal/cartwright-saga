COMPOSE = docker compose -f deploy/compose/docker-compose.yml

proto:
	buf lint && buf generate

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down -v

test:
	cd services/orchestrator && go test ./...
	cd services/notifier && uv run pytest
	cd services/billing && dotnet test

lint:
	cd services/orchestrator && golangci-lint run
	cd services/notifier && uv run ruff check .
	cd services/billing && dotnet format --verify-no-changes

.PHONY: proto up down test lint
e2e:
	$(COMPOSE) up -d --build
	cd tests/e2e && go test -v -count=1 ./...

demo-failure: e2e
