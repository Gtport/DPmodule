BINARY      := server
MODULE      := github.com/Gtport/DPmodule
MIGRATE_DIR := migrations

.PHONY: run run-local build test lint tidy \
        migrate-up migrate-up-local migrate-down migrate-down-local migrate-drop

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

# ---- migrations ----
# Цели БЕЗ суффикса берут PG_DSN из окружения: туда можно подставить любую базу,
# включая боевую — так их и запускают на VPS.
# На машине разработчика пользуйтесь *-local: они ходят только в локальный кластер
# (localhost:5433), пароль берут из .env и в строку подключения его не кладут —
# lib/pq подхватывает PGPASSWORD. Промахнуться базой ими нельзя.
# search_path указывать не нужно: он задан на уровне базы (dpport, public), поэтому
# golang-migrate находит dpport.schema_migrations сам.
PG_DSN_LOCAL := postgres://gtport_app@localhost:5433/dpport?sslmode=disable

migrate-up:
	go run ./cmd/migrate/... -dir $(MIGRATE_DIR) up

migrate-up-local:
	set -a; . ./.env; set +a; PGPASSWORD="$$PG_PASSWORD" \
	PG_DSN="$(PG_DSN_LOCAL)" go run ./cmd/migrate/... -dir $(MIGRATE_DIR) up

migrate-down:
	go run ./cmd/migrate/... -dir $(MIGRATE_DIR) down

# Откатывает РОВНО ОДИН шаг (m.Steps(-1)) — для перепроверки своей миграции.
migrate-down-local:
	set -a; . ./.env; set +a; PGPASSWORD="$$PG_PASSWORD" \
	PG_DSN="$(PG_DSN_LOCAL)" go run ./cmd/migrate/... -dir $(MIGRATE_DIR) down

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
