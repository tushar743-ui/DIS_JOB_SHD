module github.com/tushar/dis-job-queue/api

go 1.25.1

require (
	github.com/coder/websocket v1.8.15
	github.com/go-chi/chi/v5 v5.3.2
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.2
	github.com/joho/godotenv v1.5.1
	github.com/redis/go-redis/v9 v9.22.0
	github.com/rs/zerolog v1.35.1
	golang.org/x/crypto v0.55.0
)

require github.com/stretchr/testify v1.11.1 // indirect

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/tushar/dis-job-queue/shared v0.0.0
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sync v0.22.0
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)

replace github.com/tushar/dis-job-queue/shared => ../shared
