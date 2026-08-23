package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func NewPool(url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Serverless Postgres (Neon and friends) suspends when idle, and the connection
	// that wakes it can take longer than a single ping timeout. Retry until the
	// deadline instead of failing the whole process on a cold start.
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
