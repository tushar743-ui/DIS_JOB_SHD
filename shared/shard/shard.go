package shard

import (
	"context"
	"hash/fnv"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const MaxShards = 64

func AssignSQL(partitionKeyArg, idArg string) string {
	return `CASE WHEN q.shard_count <= 1 THEN 0
		ELSE mod(hashtext(COALESCE(` + partitionKeyArg + `::text, ` + idArg + `::text)) & 2147483647, q.shard_count) END`
}

func mix(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

func score(member string, shard int) uint64 {
	h := fnv.New64a()
	h.Write([]byte(member))
	return mix(h.Sum64() ^ mix(uint64(shard)+0x9e3779b97f4a7c15))
}

func Owned(self string, members []string, shardCount int) []int {
	if shardCount <= 0 {
		return nil
	}
	if shardCount > MaxShards {
		shardCount = MaxShards
	}

	live := make([]string, 0, len(members)+1)
	seen := map[string]bool{}
	for _, m := range append(members, self) {
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		live = append(live, m)
	}
	sort.Strings(live)

	owned := []int{}
	for s := 0; s < shardCount; s++ {
		best, bestScore := "", uint64(0)
		for _, m := range live {
			sc := score(m, s)
			if best == "" || sc > bestScore || (sc == bestScore && m < best) {
				best, bestScore = m, sc
			}
		}
		if best == self {
			owned = append(owned, s)
		}
	}
	return owned
}

func All(shardCount int) []int {
	if shardCount > MaxShards {
		shardCount = MaxShards
	}
	out := make([]int, 0, shardCount)
	for s := 0; s < shardCount; s++ {
		out = append(out, s)
	}
	return out
}

func membersKey(projectID string) string { return "djq:members:" + projectID }

type Registry struct {
	rdb       *redis.Client
	projectID string
	ttl       time.Duration
}

func NewRegistry(rdb *redis.Client, projectID string, ttl time.Duration) *Registry {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &Registry{rdb: rdb, projectID: projectID, ttl: ttl}
}

func (r *Registry) Heartbeat(ctx context.Context, workerID string) ([]string, error) {
	key := membersKey(r.projectID)
	now := time.Now()
	cutoff := now.Add(-r.ttl).UnixMilli()

	pipe := r.rdb.TxPipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixMilli()), Member: workerID})
	pipe.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(cutoff, 10))
	list := pipe.ZRange(ctx, key, 0, -1)
	pipe.Expire(ctx, key, r.ttl*4)

	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	return list.Val(), nil
}

func (r *Registry) Leave(ctx context.Context, workerID string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return r.rdb.ZRem(ctx, membersKey(r.projectID), workerID).Err()
}
