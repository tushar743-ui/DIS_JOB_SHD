FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.work go.work.sum* ./
COPY api/go.mod api/go.sum ./api/
COPY worker/go.mod worker/go.sum ./worker/
RUN cd api && go mod download
COPY api ./api
COPY worker ./worker
RUN cd api && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/api ./cmd/api

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
