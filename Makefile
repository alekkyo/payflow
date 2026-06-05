-include .env
export

# OrbStack and Docker Desktop both put the docker CLI here
export PATH := $(HOME)/.orbstack/bin:/Applications/Docker.app/Contents/Resources/bin:$(PATH)

BINARY_API    := bin/api
BINARY_WORKER := bin/worker

.PHONY: dev api worker migrate migrate-down test test-race lint build stripe-listen clean frontend frontend-install

## dev: start all Docker infrastructure services
dev:
	docker compose up -d postgres redis prometheus grafana

## api: run the API service
api:
	go run ./cmd/api

## worker: run the worker service
worker:
	go run ./cmd/worker

## migrate: apply all pending database migrations
migrate:
	go run ./cmd/migrate up

## migrate-down: roll back all database migrations
migrate-down:
	go run ./cmd/migrate down

## test: run all tests
test:
	go test ./...

## test-race: run all tests with the race detector
test-race:
	go test -race ./...

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## build: compile both binaries into bin/
build:
	go build -o $(BINARY_API) ./cmd/api
	go build -o $(BINARY_WORKER) ./cmd/worker

## frontend-install: install frontend npm dependencies
frontend-install:
	cd frontend && npm install

## frontend: start the Vite dev server (proxies /api/* to localhost:8080)
frontend:
	cd frontend && npm run dev

## stripe-listen: forward Stripe webhooks to localhost (requires Stripe CLI installed)
stripe-listen:
	stripe listen --forward-to localhost:8080/webhooks/stripe

## clean: remove compiled binaries
clean:
	rm -rf bin/
