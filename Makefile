.PHONY: all help test test-race build cross-build clean fmt lint up down restart logs status

# Default target
all: test build

# Output binary directory
BIN_DIR := bin

## help: Display this help message
help:
	@echo "Blue Polar Bear - Tactical Edge Monorepo"
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'

## test: Run all Go unit and integration tests
test:
	go test -count=1 -v ./...

## test-race: Run all tests with Go race detector enabled
test-race:
	go test -count=1 -v -race ./...

## build: Compile all production service binaries into ./bin
build:
	@mkdir -p $(BIN_DIR)
	@echo "==> Compiling cmd/cds-guard..."
	go build -o $(BIN_DIR)/cds-guard ./cmd/cds-guard
	@echo "==> Compiling cmd/c2-gateway..."
	go build -o $(BIN_DIR)/c2-gateway ./cmd/c2-gateway
	@echo "==> Compiling cmd/edge-agent..."
	go build -o $(BIN_DIR)/edge-agent ./cmd/edge-agent
	@echo "==> Compiling cmd/bench-check..."
	go build -o $(BIN_DIR)/bench-check ./cmd/bench-check
	@echo "==> All binaries successfully compiled to $(BIN_DIR)/"

## cross-build: Cross-compile static Linux ARM64 and AMD64 binaries for drone fleet flashing
cross-build:
	@mkdir -p $(BIN_DIR)/linux_arm64 $(BIN_DIR)/linux_amd64
	@echo "==> Cross-compiling for Linux ARM64 (Raspberry Pi 5 / Jetson)..."
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/linux_arm64/edge-agent ./cmd/edge-agent
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/linux_arm64/cds-guard ./cmd/cds-guard
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/linux_arm64/c2-gateway ./cmd/c2-gateway
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/linux_arm64/bench-check ./cmd/bench-check
	@echo "==> Cross-compiling for Linux AMD64 (GCS / x86)..."
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/linux_amd64/edge-agent ./cmd/edge-agent
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/linux_amd64/cds-guard ./cmd/cds-guard
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/linux_amd64/c2-gateway ./cmd/c2-gateway
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/linux_amd64/bench-check ./cmd/bench-check
	@echo "==> Multi-arch fleet binaries compiled to $(BIN_DIR)/"

## lint: Check Go code formatting and style
lint:
	@test -z "$$(gofmt -s -l . | grep -v '^vendor/' | tee /dev/stderr)" || (echo "Format check failed. Run 'make fmt' to fix." && exit 1)

## fmt: Automatically format all Go source code
fmt:
	gofmt -s -w .

## up: Build and launch the multi-container tactical mesh
up:
	docker compose up -d --build

## down: Stop and remove all tactical mesh containers
down:
	docker compose down

## restart: Restart the tactical mesh stack
restart: down up

## logs: Tail live output across all running containers
logs:
	docker compose logs -f

## status: Check health and running status of containers
status:
	docker compose ps

## clean: Remove build artifacts, spooled logs, and binaries
clean:
	rm -rf $(BIN_DIR)
	rm -rf logs/*.jsonl
	@echo "==> Cleaned build artifacts and spool logs."
