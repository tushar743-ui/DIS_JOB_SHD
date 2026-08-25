FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.work go.work.sum* ./
COPY api/go.mod api/go.sum ./api/
COPY shared/go.mod shared/go.sum ./shared/
COPY worker/go.mod worker/go.sum ./worker/
RUN cd worker && go mod download
COPY api ./api
COPY shared ./shared
COPY worker ./worker
RUN cd worker && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/worker ./cmd/worker

FROM gcr.io/distroless/static-debian12
COPY --from=builder /bin/worker /worker
EXPOSE 10000
ENTRYPOINT ["/worker"]
