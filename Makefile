.PHONY: all build build-api build-worker test test-go test-web lint migrate \
	dev dev-watch dev-api dev-worker dev-web clean

all: build


build: build-api build-worker

build-api:
	cd api && go build -o ../bin/api ./cmd/api

build-worker:
	cd worker && go build -o ../bin/worker ./cmd/worker


test: test-go test-web

test-go:
	cd api && go test ./... -timeout 120s
	cd worker && go test ./... -timeout 120s

test-web:
	cd web && npm test

test-integration:
	go test ./api/... ./worker/... -v -timeout 120s -tags integration


lint:
	cd api && go vet ./...
	cd worker && go vet ./...


DATABASE_URL ?= $(shell sed -n "s/^DATABASE_URL=//p" .env 2>/dev/null | tr -d "'\"")
MIGRATE ?= $(shell command -v migrate 2>/dev/null || echo $(HOME)/.local/bin/migrate)

migrate-up:
	$(MIGRATE) -path db/migrations -database "$(DATABASE_URL)" up

migrate-down:
	$(MIGRATE) -path db/migrations -database "$(DATABASE_URL)" down 1


dev:
	@exec ./scripts/dev.sh

dev-watch:
	@exec ./scripts/dev.sh --watch

dev-api:
	@exec ./scripts/dev.sh --api-only

dev-worker:
	@exec ./scripts/dev.sh --worker-only

dev-web:
	@exec ./scripts/dev.sh --web-only


swagger:
	cd api && swag init -g cmd/api/main.go -o ./docs


docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f


clean:
	rm -rf bin/
