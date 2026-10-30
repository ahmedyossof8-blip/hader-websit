# ==============================================================================
# HADER-V2: Production Operational Automation Makefile
# High-Performance Go, Caddy Reverse Proxy, Migrations, and k6 Stress Testing
# ==============================================================================

# Variables
BINARY_NAME=server
BUILD_DIR=bin
CMD_PATH=./cmd/api/main.go
DOCKER_IMAGE=hader-backend:v2.0.0
MIGRATIONS_DIR=./migrations

# Automatically load .env if present
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

.PHONY: all build run dev test test-race lint clean docker-build docker-up docker-down docker-logs migrate-up migrate-down stress-checkin stress-auth help

all: build

## build: Compile production static binary with stripped debug symbols and trimmed paths
build:
	@echo "==> Compiling static Linux/Windows Go binary..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="-w -s" -trimpath -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_PATH)
	@echo "==> Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

## run: Run the API server directly
run:
	@echo "==> Starting Hader-V2 backend server..."
	go run $(CMD_PATH)

## dev: Run with Air hot-reloader if installed, fallback to go run
dev:
	@if command -v air > /dev/null 2>&1; then \
		air; \
	else \
		echo "==> Air not installed. Starting with standard go run..."; \
		go run $(CMD_PATH); \
	fi

## test: Run all unit and integration tests across all modules
test:
	@echo "==> Executing unit test suite..."
	go test -v ./...

## test-race: Run test suite with Go data race detector enabled
test-race:
	@echo "==> Executing tests with -race detector..."
	go test -race -v ./...

## lint: Run Go vet static code analyzer
lint:
	@echo "==> Running static analysis (go vet)..."
	go vet ./...

## clean: Remove compiled binaries and purge test cache
clean:
	@echo "==> Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	go clean -testcache

## docker-build: Build the ultra-lightweight multi-stage Docker image (< 20MB)
docker-build:
	@echo "==> Building Docker image $(DOCKER_IMAGE)..."
	docker build -t $(DOCKER_IMAGE) .

## docker-up: Launch full container stack (Backend + Caddy Edge Proxy)
docker-up:
	@echo "==> Starting production containers in background..."
	docker compose up -d --build

## docker-down: Gracefully stop and clean up containers
docker-down:
	@echo "==> Halting production container stack..."
	docker compose down --remove-orphans

## docker-logs: Tail live aggregate container logs
docker-logs:
	docker compose logs -f

## migrate-up: Apply pending PostgreSQL DDL migrations
migrate-up:
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "ERROR: DATABASE_URL must be defined in environment or .env"; exit 1; \
	fi
	@echo "==> Running PostgreSQL UP migrations..."
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" -verbose up

## migrate-down: Rollback the last PostgreSQL DDL migration
migrate-down:
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "ERROR: DATABASE_URL must be defined in environment or .env"; exit 1; \
	fi
	@echo "==> Rolling back last PostgreSQL migration..."
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" -verbose down 1

## stress-checkin: Execute k6 attendance stress test (1,500 VUs in 30s rush)
stress-checkin:
	@echo "==> Launching k6 attendance check-in stress simulation..."
	k6 run tests/stress/checkin_stress.js

## stress-auth: Execute k6 dual-role login stress test
stress-auth:
	@echo "==> Launching k6 dual-role authentication stress simulation..."
	k6 run tests/stress/auth_stress.js

## help: Display available automation commands
help:
	@echo "Hader-V2 Production Automation Pipeline:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST)
