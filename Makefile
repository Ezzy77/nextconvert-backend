.PHONY: build run test clean docker-build docker-up docker-down migrate

# Build variables
BINARY_NAME=nextconvert
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Go commands
GOCMD=go
GOBUILD=$(GOCMD) build
GORUN=$(GOCMD) run
GOTEST=$(GOCMD) test
GOCLEAN=$(GOCMD) clean
GOMOD=$(GOCMD) mod

# Build targets
build: build-server build-worker

build-server:
	$(GOBUILD) $(LDFLAGS) -o bin/server ./cmd/server

build-worker:
	$(GOBUILD) $(LDFLAGS) -o bin/worker ./cmd/worker

# Run targets
run-server:
	$(GORUN) ./cmd/server

run-worker:
	$(GORUN) ./cmd/worker

# Test
test:
	$(GOTEST) -v -race ./...

test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Clean
clean:
	$(GOCLEAN)
	rm -rf bin/
	rm -f coverage.out coverage.html

# Dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Docker commands
docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

docker-restart:
	docker compose restart

# Development
dev: docker-up run-server

# Database migrations
migrate-up:
	$(GORUN) ./cmd/migrate up

migrate-down:
	$(GORUN) ./cmd/migrate down

# Linting
lint:
	golangci-lint run ./...

# Generate
generate:
	$(GOCMD) generate ./...

# Help
help:
	@echo "Available commands:"
	@echo "  build          - Build server and worker binaries"
	@echo "  run-server     - Run the API server"
	@echo "  run-worker     - Run the job worker"
	@echo "  test           - Run tests"
	@echo "  docker-up      - Start Docker containers"
	@echo "  docker-down    - Stop Docker containers"
	@echo "  migrate-up     - Run database migrations"
	@echo "  lint           - Run linter"
