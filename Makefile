.PHONY: build run migrate-up migrate-down migrate-create docker-up docker-down tidy

# --- Variables ---
DB_URL ?= postgres://tgbot:tgbot@localhost:5432/tgbot?sslmode=disable
MIGRATIONS_DIR = migrations

# --- Go ---
build:
	go build -o bin/bot ./cmd/bot

run: build
	./bin/bot

tidy:
	go mod tidy

# --- Docker ---
docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f bot

# --- Migrations (golang-migrate) ---
# Install: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

migrate-up:
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" up

migrate-down:
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" down

migrate-down-one:
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" down 1

migrate-create:
	@read -p "Migration name: " name; \
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $$name

migrate-version:
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" version

migrate-force:
	@read -p "Version: " ver; \
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_URL)" force $$ver
