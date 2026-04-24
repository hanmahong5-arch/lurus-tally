.PHONY: build test lint run docker-build clean coverage dev dev-web dev-stop migrate-up migrate-down seed test-integration

# Build version from git tag or commit; fall back to "dev".
BUILD_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Docker Compose file for local development.
COMPOSE_FILE ?= docker-compose.dev.yml

# Load .env into Make variables (soft include — no error if file absent).
-include .env
export

build:
	CGO_ENABLED=0 go build -ldflags="-s -w -X github.com/hanmahong5-arch/lurus-tally/internal/pkg/version.Version=$(BUILD_VERSION)" -trimpath -o tally-backend ./cmd/server

test:
	go test -count=1 ./...

lint:
	golangci-lint run ./...

run:
	go run ./cmd/server

docker-build:
	docker build --build-arg BUILD_VERSION=$(BUILD_VERSION) -t lurus-tally:local .

clean:
	rm -f tally-backend coverage.out

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

# ── Local dev ──────────────────────────────────────────────────────────────────

dev: dev-stop
	docker compose -f $(COMPOSE_FILE) up -d --wait
	@echo "Services healthy — starting Tally backend on :18200..."
	go run ./cmd/server

dev-web:
	cd web && bun install && bun run dev

dev-stop:
	-docker compose -f $(COMPOSE_FILE) down

# ── Database migrations ────────────────────────────────────────────────────────

migrate-up:
	go run -tags 'pgx5' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.1 \
		-database "$(DATABASE_DSN)" -path migrations up

migrate-down:
	go run -tags 'pgx5' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.1 \
		-database "$(DATABASE_DSN)" -path migrations down 1

# ── Seed & integration tests ───────────────────────────────────────────────────

seed:
	@echo "seed: no-op in MVP stage"

test-integration:
	go test -v -count=1 -tags=integration -race -timeout=120s ./...
