BINARY      := server
MODULE      := github.com/Gtport/DPmodule
MIGRATE_DIR := migrations

.PHONY: run run-local build test lint migrate-up migrate-down tidy

run:
	go run ./cmd/server/... -config config.yaml

# Локальная разработка (WSL): секреты из .env + свой конфиг.
# На сервере переменные окружения подставляет systemd (EnvironmentFile=.env),
# а при ручном запуске их надо внести самому — иначе старт падает с
# «secret PG_PASSWORD is required when postgres.enabled is true».
run-local:
	set -a; . ./.env; set +a; go run ./cmd/server/... -config config.local.yaml

build:
	go build -o bin/$(BINARY) ./cmd/server/...

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

tidy:
	go mod tidy

# ---- migrations (requires PG_DSN) ----
migrate-up:
	go run ./cmd/migrate/... -dir $(MIGRATE_DIR) up

migrate-down:
	go run ./cmd/migrate/... -dir $(MIGRATE_DIR) down

migrate-drop:
	go run ./cmd/migrate/... -dir $(MIGRATE_DIR) drop

# ---- docker ----
docker-up:
	docker compose -f deployments/docker-compose.yml up -d --build

docker-down:
	docker compose -f deployments/docker-compose.yml down

docker-logs:
	docker compose -f deployments/docker-compose.yml logs -f app

# ---- swagger (requires swag CLI) ----
swagger:
	swag init -g cmd/server/main.go -o api/swagger --parseDependency --parseInternal
