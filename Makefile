.PHONY: all build build-api build-worker test lint migrate dev clean

all: build


build: build-api build-worker

build-api:
	cd api && go build -o ../bin/api ./cmd/api

build-worker:
	cd worker && go build -o ../bin/worker ./cmd/worker


test:
	go test ./api/... ./worker/... -v -timeout 120s

test-integration:
	go test ./api/... ./worker/... -v -timeout 120s -tags integration


lint:
	cd api && go vet ./...
	cd worker && go vet ./...


migrate-up:
	$(HOME)/.local/bin/migrate -path db/migrations -database "$(DATABASE_URL)" up

migrate-down:
	$(HOME)/.local/bin/migrate -path db/migrations -database "$(DATABASE_URL)" down 1


dev:
	docker compose up -d postgres redis
	@echo "Waiting for postgres..."
	@sleep 2
	$(MAKE) migrate-up
	air -c .air.toml &
	cd web && npm run dev

dev-api:
	cd api && air

dev-worker:
	cd worker && go run ./cmd/worker


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
