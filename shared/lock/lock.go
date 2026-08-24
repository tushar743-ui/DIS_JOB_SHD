package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrNotAcquired = errors.New("lock: not acquired")
	ErrLost        = errors.New("lock: ownership lost")
)

var (
	releaseScript = redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		end
		return 0`)

	refreshScript = redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		end
		return 0`)
)

type Lock struct {
	rdb   *redis.Client
	key   string
	token string
	ttl   time.Duration
	fence int64
}

func (l *Lock) Key() string        { return l.key }
func (l *Lock) Token() string      { return l.token }
func (l *Lock) Fence() int64       { return l.fence }
func (l *Lock) TTL() time.Duration { return l.ttl }

func Acquire(ctx context.Context, rdb *redis.Client, key string, ttl time.Duration) (*Lock, error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}

	ok, err := rdb.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotAcquired
	}

	fence, err := rdb.Incr(ctx, key+":fence").Result()
	if err != nil {
		releaseScript.Run(ctx, rdb, []string{key}, token)
		return nil, err
	}

	return &Lock{rdb: rdb, key: key, token: token, ttl: ttl, fence: fence}, nil
}

func (l *Lock) Refresh(ctx context.Context) error {
	n, err := refreshScript.Run(ctx, l.rdb, []string{l.key}, l.token, l.ttl.Milliseconds()).Int64()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLost
	}
	return nil
}

func (l *Lock) Release(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return releaseScript.Run(ctx, l.rdb, []string{l.key}, l.token).Err()
}

type GuardOptions struct {
	TTL           time.Duration
	RetryInterval time.Duration
	OnAcquired    func(*Lock)
	OnLost        func(error)
	OnUnavailable func(error)
}

func (o GuardOptions) withDefaults() GuardOptions {
	if o.TTL <= 0 {
		o.TTL = 30 * time.Second
	}
	if o.RetryInterval <= 0 {
		o.RetryInterval = o.TTL / 3
	}
	if o.OnAcquired == nil {
		o.OnAcquired = func(*Lock) {}
	}
	if o.OnLost == nil {
		o.OnLost = func(error) {}
	}
	if o.OnUnavailable == nil {
		o.OnUnavailable = func(error) {}
	}
	return o
}

func Guard(ctx context.Context, rdb *redis.Client, key string, opts GuardOptions, fn func(context.Context, *Lock)) {
	opts = opts.withDefaults()
	ticker := time.NewTicker(opts.RetryInterval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		l, err := Acquire(ctx, rdb, key, opts.TTL)
		switch {
		case err == nil:
			opts.OnAcquired(l)
			runHeld(ctx, l, opts, fn)
		case errors.Is(err, ErrNotAcquired):
		default:
			opts.OnUnavailable(err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runHeld(ctx context.Context, l *Lock, opts GuardOptions, fn func(context.Context, *Lock)) {
	held, release := context.WithCancel(ctx)
	defer release()

	done := make(chan struct{})
	go func() {
		defer close(done)
		renew := time.NewTicker(l.ttl / 3)
		defer renew.Stop()
		for {
			select {
			case <-held.Done():
				return
			case <-renew.C:
				if err := l.Refresh(held); err != nil {
					opts.OnLost(err)
					release()
					return
				}
			}
		}
	}()

	fn(held, l)
	release()
	<-done
	l.Release(context.WithoutCancel(ctx))
}

func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
