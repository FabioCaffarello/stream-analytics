package jetstream

import (
	"fmt"
	"math"
	"testing"
)

// TestShardKey_Deterministic verifies that the same subject always produces
// the same shard key across multiple calls.
func TestShardKey_Deterministic(t *testing.T) {
	subjects := []string{
		"marketdata.bookdelta.v1.binance.BTCUSDT",
		"marketdata.trade.v1.bybit.ETHUSDT",
		"aggregation.snapshot.v1.binance.BTCUSDT",
		"insights.spread.v1.binance.BTCUSDT",
	}
	for _, subject := range subjects {
		k1 := ShardKey(subject)
		k2 := ShardKey(subject)
		if k1 != k2 {
			t.Errorf("ShardKey(%q) = %d on first call, %d on second: not deterministic", subject, k1, k2)
		}
	}
}

// TestShardKey_SameVenueInstrument_DifferentEventType verifies that the shard
// key is identical for the same venue+instrument regardless of event type or
// version.  This guarantees all order-book state transitions for one instrument
// land on the same shard.
func TestShardKey_SameVenueInstrument_DifferentEventType(t *testing.T) {
	base := ShardKey("marketdata.bookdelta.v1.binance.BTCUSDT")
	same := []string{
		"marketdata.trade.v1.binance.BTCUSDT",
		"aggregation.snapshot.v1.binance.BTCUSDT",
		"marketdata.bookdelta.v2.binance.BTCUSDT",
	}
	for _, s := range same {
		k := ShardKey(s)
		if k != base {
			t.Errorf("ShardKey(%q) = %d; want %d (same venue+instrument)", s, k, base)
		}
	}
}

// TestShardKey_DifferentVenueInstrument verifies that distinct venue+instrument
// pairs produce (with very high probability) distinct keys.
func TestShardKey_DifferentVenueInstrument(t *testing.T) {
	pairs := []string{
		"marketdata.bookdelta.v1.binance.BTCUSDT",
		"marketdata.bookdelta.v1.binance.ETHUSDT",
		"marketdata.bookdelta.v1.bybit.BTCUSDT",
		"marketdata.bookdelta.v1.bybit.ETHUSDT",
	}
	seen := make(map[uint32]string)
	for _, subject := range pairs {
		k := ShardKey(subject)
		if prev, exists := seen[k]; exists {
			t.Errorf("ShardKey collision: %q and %q both map to %d", prev, subject, k)
		}
		seen[k] = subject
	}
}

// TestShardKey_FallbackOnInvalidSubject verifies that subjects with fewer than
// 4 tokens do not panic and return a stable (non-zero) value.
func TestShardKey_FallbackOnInvalidSubject(t *testing.T) {
	malformed := []string{
		"",
		"onlyone",
		"two.tokens",
		"three.tokens.only",
	}
	for _, s := range malformed {
		k1 := ShardKey(s)
		k2 := ShardKey(s)
		if k1 != k2 {
			t.Errorf("ShardKey(%q) not deterministic on fallback path", s)
		}
	}
}

// TestShardGroup_Count0Or1_AlwaysZero verifies that degenerate group counts
// always return group 0 (sharding disabled).
func TestShardGroup_Count0Or1_AlwaysZero(t *testing.T) {
	keys := []uint32{0, 1, 7, 42, math.MaxUint32}
	for _, count := range []int{0, 1} {
		for _, key := range keys {
			if g := ShardGroup(key, count); g != 0 {
				t.Errorf("ShardGroup(%d, %d) = %d; want 0", key, count, g)
			}
		}
	}
}

// TestShardGroup_Coverage verifies that with groupCount N, all group IDs
// [0, N) are reachable (no dead shards) for a representative set of keys.
func TestShardGroup_Coverage(t *testing.T) {
	for _, count := range []int{2, 3, 4, 8} {
		seen := make(map[int]bool, count)
		limit := uint32(count * 64) // #nosec G115 -- count is <= 8; product fits uint32
		for i := uint32(0); i < limit; i++ {
			seen[ShardGroup(i, count)] = true
		}
		for g := 0; g < count; g++ {
			if !seen[g] {
				t.Errorf("ShardGroup with count=%d never produced group %d", count, g)
			}
		}
	}
}

// TestShardGroup_InRange verifies that ShardGroup always returns a value in
// [0, groupCount).
func TestShardGroup_InRange(t *testing.T) {
	for count := 2; count <= 16; count++ {
		for key := uint32(0); key < 256; key++ {
			g := ShardGroup(key, count)
			if g < 0 || g >= count {
				t.Errorf("ShardGroup(%d, %d) = %d; out of range [0, %d)", key, count, g, count)
			}
		}
	}
}

// TestShardGroup_UnionCoversAllMessages verifies that, for a concrete set of
// subjects, the union of all groups covers exactly all messages with no overlap.
func TestShardGroup_UnionCoversAllMessages(t *testing.T) {
	subjects := []string{
		"marketdata.bookdelta.v1.binance.BTCUSDT",
		"marketdata.bookdelta.v1.binance.ETHUSDT",
		"marketdata.bookdelta.v1.bybit.BTCUSDT",
		"marketdata.bookdelta.v1.bybit.ETHUSDT",
		"marketdata.trade.v1.binance.BTCUSDT",
		"marketdata.trade.v1.bybit.ETHUSDT",
	}
	const groupCount = 2

	// groupMessages[g] holds the subjects assigned to group g.
	groupMessages := make([][]string, groupCount)
	for _, s := range subjects {
		g := ShardGroup(ShardKey(s), groupCount)
		groupMessages[g] = append(groupMessages[g], s)
	}

	// Union must equal the full subject set.
	total := 0
	for g := 0; g < groupCount; g++ {
		total += len(groupMessages[g])
	}
	if total != len(subjects) {
		t.Errorf("union of all groups has %d subjects; want %d", total, len(subjects))
	}

	// Each subject must appear in exactly one group.
	assigned := make(map[string]int)
	for g := 0; g < groupCount; g++ {
		for _, s := range groupMessages[g] {
			assigned[s]++
		}
	}
	for _, s := range subjects {
		if assigned[s] != 1 {
			t.Errorf("subject %q appears in %d groups; want exactly 1", s, assigned[s])
		}
	}
}

// TestShardKey_StableAcrossGroups verifies the combined contract: same subject
// always ends up in the same group regardless of how many times it is computed.
func TestShardKey_StableAcrossGroups(t *testing.T) {
	cases := []struct {
		subject    string
		groupCount int
	}{
		{"marketdata.bookdelta.v1.binance.BTCUSDT", 1},
		{"marketdata.bookdelta.v1.binance.BTCUSDT", 2},
		{"marketdata.bookdelta.v1.binance.BTCUSDT", 8},
		{"marketdata.trade.v1.bybit.ETHUSDT", 4},
	}
	for _, tc := range cases {
		name := fmt.Sprintf("%s/count=%d", tc.subject, tc.groupCount)
		t.Run(name, func(t *testing.T) {
			first := ShardGroup(ShardKey(tc.subject), tc.groupCount)
			for i := 0; i < 100; i++ {
				got := ShardGroup(ShardKey(tc.subject), tc.groupCount)
				if got != first {
					t.Fatalf("iteration %d: ShardGroup = %d; want %d", i, got, first)
				}
			}
		})
	}
}
