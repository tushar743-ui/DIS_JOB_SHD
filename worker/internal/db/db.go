package db

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func NewPool(url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 1; ; attempt++ {
		pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
		lastErr = pool.Ping(pingCtx)
		pingCancel()
		if lastErr == nil {
			return pool, nil
		}
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, fmt.Errorf("database unreachable after %d attempts: %w", attempt, lastErr)
		case <-time.After(2 * time.Second):
		}
	}
}

func NewRedis(url string) *redis.Client {
	opts, err := redis.ParseURL(url)
	if err != nil {
		panic("invalid redis url: " + err.Error())
	}
	return redis.NewClient(opts)
}

func MarkWorkerOffline(ctx context.Context, pool *pgxpool.Pool, workerID string) {
	pool.Exec(ctx,
		`UPDATE workers SET status='offline', last_heartbeat_at=now() WHERE id=$1`, workerID)
}

func RegisterWorker(ctx context.Context, pool *pgxpool.Pool, projectID string, concurrency int, queueIDs []string) (string, error) {
	hostname, _ := os.Hostname()
	pid := os.Getpid()

	addrs, _ := net.LookupHost(hostname)
	_ = addrs

	var workerID string
	err := pool.QueryRow(ctx,
		`INSERT INTO workers (project_id, hostname, pid, status, concurrency)
		 VALUES ($1,$2,$3,'active',$4)
		 RETURNING id`,
		projectID, hostname, pid, concurrency,
	).Scan(&workerID)
	return workerID, err
}
