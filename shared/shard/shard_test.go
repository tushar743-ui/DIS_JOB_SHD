package shard

import (
	"fmt"
	"math"
	"testing"
)

func TestOwnedPartitionsShardSpaceExactlyOnce(t *testing.T) {
	tests := []struct {
		members    int
		shardCount int
	}{
		{1, 1}, {1, 16}, {2, 16}, {3, 16}, {5, 8}, {8, 8}, {16, 4}, {4, 64},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%dworkers_%dshards", tc.members, tc.shardCount), func(t *testing.T) {
			members := workerIDs(tc.members)

			ownerOf := map[int]string{}
			for _, self := range members {
				for _, s := range Owned(self, members, tc.shardCount) {
					if prev, dup := ownerOf[s]; dup {
						t.Fatalf("shard %d claimed by both %s and %s", s, prev, self)
					}
					ownerOf[s] = self
				}
			}

			if len(ownerOf) != tc.shardCount {
				t.Fatalf("shard coverage: got %d shards owned, want %d", len(ownerOf), tc.shardCount)
			}
		})
	}
}

func TestOwnedIsDeterministic(t *testing.T) {
	members := workerIDs(4)
	first := Owned(members[1], members, 32)
	for range 20 {
		if got := Owned(members[1], members, 32); !equal(got, first) {
			t.Fatalf("ownership drifted between calls: %v vs %v", got, first)
		}
	}
}

func TestOwnedIgnoresMemberOrdering(t *testing.T) {
	members := workerIDs(5)
	shuffled := []string{members[3], members[0], members[4], members[1], members[2]}

	for _, self := range members {
		if a, b := Owned(self, members, 24), Owned(self, shuffled, 24); !equal(a, b) {
			t.Fatalf("ownership for %s depends on member order: %v vs %v", self, a, b)
		}
	}
}

func TestOwnedIncludesSelfWhenMissingFromRegistry(t *testing.T) {
	members := workerIDs(3)
	owned := Owned("worker-unregistered", members, 64)
	if len(owned) == 0 {
		t.Fatal("a worker absent from the registry snapshot must still claim its share")
	}
}

func TestOwnedMinimisesReshufflingWhenAMemberLeaves(t *testing.T) {
	const shards = 64
	members := workerIDs(4)
	survivors := members[:3]

	moved, kept := 0, 0
	for _, self := range survivors {
		before := set(Owned(self, members, shards))
		after := Owned(self, survivors, shards)
		for _, s := range after {
			if before[s] {
				kept++
			} else {
				moved++
			}
		}
	}

	if kept == 0 {
		t.Fatal("every shard moved after one member left, rendezvous hashing is not being used")
	}
	if moved > shards/3 {
		t.Fatalf("reshuffled %d of %d shards after losing one of four members, want at most %d",
			moved, shards, shards/3)
	}
}

func TestOwnedSoleMemberTakesEverything(t *testing.T) {
	owned := Owned("only-worker", []string{"only-worker"}, 16)
	if len(owned) != 16 {
		t.Fatalf("sole worker owns %d shards, want 16", len(owned))
	}
}

func TestOwnedHandlesEmptyShardSpace(t *testing.T) {
	if got := Owned("w1", []string{"w1"}, 0); got != nil {
		t.Fatalf("zero shards should yield no ownership, got %v", got)
	}
}

func TestOwnedCapsAtMaxShards(t *testing.T) {
	owned := Owned("w1", []string{"w1"}, MaxShards*4)
	if len(owned) != MaxShards {
		t.Fatalf("owned %d shards, want the %d shard cap", len(owned), MaxShards)
	}
}

func TestOwnedSkipsBlankAndDuplicateMembers(t *testing.T) {
	noisy := []string{"w1", "", "w2", "w1", "w2"}
	clean := []string{"w1", "w2"}

	for _, self := range clean {
		if a, b := Owned(self, noisy, 16), Owned(self, clean, 16); !equal(a, b) {
			t.Fatalf("blank or duplicate registry entries changed ownership for %s: %v vs %v", self, a, b)
		}
	}
}

func TestAll(t *testing.T) {
	if got := All(3); !equal(got, []int{0, 1, 2}) {
		t.Fatalf("All(3) = %v, want [0 1 2]", got)
	}
	if got := All(MaxShards * 2); len(got) != MaxShards {
		t.Fatalf("All caps at %d, got %d", MaxShards, len(got))
	}
}

func workerIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("worker-%02d-%s", i, "3f9a1c")
	}
	return out
}

func set(in []int) map[int]bool {
	out := make(map[int]bool, len(in))
	for _, v := range in {
		out[v] = true
	}
	return out
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestOwnedDistributesShardsWithoutStarvation(t *testing.T) {
	const (
		shards  = 64
		workers = 8
		ideal   = shards / workers
	)
	members := workerIDs(workers)

	total := 0
	for _, self := range members {
		got := len(Owned(self, members, shards))
		total += got
		if got == 0 {
			t.Errorf("%s owns no shards at all, it would sit idle", self)
		}
		if got > ideal*3 {
			t.Errorf("%s owns %d of %d shards, far beyond the %d ideal", self, got, shards, ideal)
		}
	}
	if total != shards {
		t.Fatalf("workers collectively own %d shards, want exactly %d", total, shards)
	}
}

func TestOwnedIsUnbiasedAcrossClusters(t *testing.T) {
	const (
		workers  = 4
		shards   = 64
		clusters = 200
		ideal    = float64(shards) / workers
	)

	sum := 0.0
	for c := range clusters {
		members := make([]string, workers)
		for i := range members {
			members[i] = fmt.Sprintf("cluster%03d-worker%02d", c, i)
		}
		sum += float64(len(Owned(members[0], members, shards)))
	}

	mean := sum / clusters
	if math.Abs(mean-ideal)/ideal > 0.05 {
		t.Fatalf("average ownership over %d clusters is %.2f shards, want %.2f (hash is biased)",
			clusters, mean, ideal)
	}
}
