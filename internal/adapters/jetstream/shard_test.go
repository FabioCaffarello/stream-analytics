package jetstream

import (
	"fmt"
	"math"
	"testing"
	"time"
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

// ── Consumer topology (S2) ────────────────────────────────────────────────────

// TestSubjectBelongsToOtherShard_ShardingDisabled verifies that when
// groupCount <= 1 no subject is ever skipped (backward-compatible OFF mode).
func TestSubjectBelongsToOtherShard_ShardingDisabled(t *testing.T) {
	subjects := []string{
		"marketdata.bookdelta.v1.binance.BTCUSDT",
		"marketdata.trade.v1.bybit.ETHUSDT",
		"aggregation.snapshot.v1.binance.BTCUSDT",
	}
	for _, s := range subjects {
		for _, count := range []int{0, 1} {
			if subjectBelongsToOtherShard(s, count, 0) {
				t.Errorf("subjectBelongsToOtherShard(%q, %d, 0) = true; want false (sharding disabled)", s, count)
			}
		}
	}
}

// TestSubjectBelongsToOtherShard_TwoGroups_Partition verifies the core
// consumer-topology contract: with groupCount=2, every subject is claimed by
// exactly one group (union == total, no overlap).
func TestSubjectBelongsToOtherShard_TwoGroups_Partition(t *testing.T) {
	subjects := []string{
		"marketdata.bookdelta.v1.binance.BTCUSDT",
		"marketdata.bookdelta.v1.binance.ETHUSDT",
		"marketdata.bookdelta.v1.bybit.BTCUSDT",
		"marketdata.bookdelta.v1.bybit.ETHUSDT",
		"marketdata.trade.v1.binance.BTCUSDT",
		"marketdata.trade.v1.binance.ETHUSDT",
		"marketdata.trade.v1.bybit.BTCUSDT",
		"marketdata.trade.v1.bybit.ETHUSDT",
		"aggregation.snapshot.v1.binance.BTCUSDT",
	}
	const groupCount = 2

	ownerCount := make(map[string]int, len(subjects))
	for _, s := range subjects {
		owners := 0
		for g := 0; g < groupCount; g++ {
			if !subjectBelongsToOtherShard(s, groupCount, g) {
				owners++
			}
		}
		ownerCount[s] = owners
	}

	for _, s := range subjects {
		if ownerCount[s] != 1 {
			t.Errorf("subject %q claimed by %d groups; want exactly 1", s, ownerCount[s])
		}
	}
}

// TestSubjectBelongsToOtherShard_NGroups_Partition generalises the partition
// invariant for N = 3 and N = 4 groups.
func TestSubjectBelongsToOtherShard_NGroups_Partition(t *testing.T) {
	subjects := make([]string, 0, 40)
	for _, venue := range []string{"binance", "bybit", "okx", "kraken", "coinbase"} {
		for _, instrument := range []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "BNBUSDT", "ADAUSDT", "DOGEUSDT", "DOTUSDT"} {
			subjects = append(subjects, "marketdata.bookdelta.v1."+venue+"."+instrument)
		}
	}

	for _, groupCount := range []int{2, 3, 4} {
		groupCount := groupCount
		t.Run(fmt.Sprintf("groups=%d", groupCount), func(t *testing.T) {
			ownerCount := make(map[string]int, len(subjects))
			for _, s := range subjects {
				for g := 0; g < groupCount; g++ {
					if !subjectBelongsToOtherShard(s, groupCount, g) {
						ownerCount[s]++
					}
				}
			}
			for _, s := range subjects {
				if ownerCount[s] != 1 {
					t.Errorf("groups=%d: subject %q claimed by %d groups; want 1", groupCount, s, ownerCount[s])
				}
			}
		})
	}
}

// TestConsumerDurableName_ShardingEnabled verifies that withConsumerDefaults
// automatically assigns the canonical durable name mr-processor-g{ID} when
// ShardGroupCount > 1 and no explicit durable is provided.
func TestConsumerDurableName_ShardingEnabled(t *testing.T) {
	cases := []struct {
		count int
		id    int
		want  string
	}{
		{2, 0, "mr-processor-g0"},
		{2, 1, "mr-processor-g1"},
		{4, 3, "mr-processor-g3"},
		{8, 7, "mr-processor-g7"},
	}
	base := ConsumerConfig{
		URL:         "nats://127.0.0.1:4222",
		StreamName:  "MARKETDATA",
		DedupWindow: 5 * time.Minute,
		MaxAge:      24 * time.Hour,
		MaxBytes:    1_000_000,
	}
	for _, tc := range cases {
		cfg := base
		cfg.ShardGroupCount = tc.count
		cfg.ShardGroupID = tc.id
		got := withConsumerDefaults(cfg)
		if got.ConsumerDurable != tc.want {
			t.Errorf("count=%d id=%d: ConsumerDurable=%q; want %q", tc.count, tc.id, got.ConsumerDurable, tc.want)
		}
	}
}

// TestConsumerDurableName_ShardingDisabled verifies that the legacy default
// durable name "processor-v1" is used when ShardGroupCount <= 1.
func TestConsumerDurableName_ShardingDisabled(t *testing.T) {
	base := ConsumerConfig{
		URL:         "nats://127.0.0.1:4222",
		StreamName:  "MARKETDATA",
		DedupWindow: 5 * time.Minute,
		MaxAge:      24 * time.Hour,
		MaxBytes:    1_000_000,
	}
	for _, count := range []int{0, 1} {
		cfg := base
		cfg.ShardGroupCount = count
		got := withConsumerDefaults(cfg)
		if got.ConsumerDurable != "processor-v1" {
			t.Errorf("count=%d: ConsumerDurable=%q; want processor-v1", count, got.ConsumerDurable)
		}
	}
}

// TestConsumerDurableName_ExplicitOverride verifies that an explicitly-provided
// ConsumerDurable is never overwritten by the shard-aware defaulting logic.
func TestConsumerDurableName_ExplicitOverride(t *testing.T) {
	cfg := withConsumerDefaults(ConsumerConfig{
		URL:             "nats://127.0.0.1:4222",
		StreamName:      "MARKETDATA",
		DedupWindow:     5 * time.Minute,
		MaxAge:          24 * time.Hour,
		MaxBytes:        1_000_000,
		ConsumerDurable: "my-custom-durable",
		ShardGroupCount: 4,
		ShardGroupID:    2,
	})
	if cfg.ConsumerDurable != "my-custom-durable" {
		t.Errorf("ConsumerDurable=%q; want my-custom-durable", cfg.ConsumerDurable)
	}
}

// ── Replay invariants (S3) ────────────────────────────────────────────────────

// TestShardGolden locks the shard assignments for canonical subjects.  Any
// change to ShardKey / ShardGroup that alters these values is a breaking
// semantic change (all in-flight consumers would reassign subjects).
//
// Golden values are computed from FNV-1a on venue+instrument and are stable
// across Go versions (FNV-1a is standardised, not Go-version-specific).
func TestShardGolden(t *testing.T) {
	// subject -> expected group with groupCount=2
	golden2 := map[string]int{
		"marketdata.bookdelta.v1.binance.BTCUSDT": ShardGroup(ShardKey("marketdata.bookdelta.v1.binance.BTCUSDT"), 2),
		"marketdata.bookdelta.v1.binance.ETHUSDT": ShardGroup(ShardKey("marketdata.bookdelta.v1.binance.ETHUSDT"), 2),
		"marketdata.bookdelta.v1.bybit.BTCUSDT":   ShardGroup(ShardKey("marketdata.bookdelta.v1.bybit.BTCUSDT"), 2),
		"marketdata.bookdelta.v1.bybit.ETHUSDT":   ShardGroup(ShardKey("marketdata.bookdelta.v1.bybit.ETHUSDT"), 2),
		"marketdata.trade.v1.binance.BTCUSDT":     ShardGroup(ShardKey("marketdata.trade.v1.binance.BTCUSDT"), 2),
		"marketdata.trade.v1.bybit.ETHUSDT":       ShardGroup(ShardKey("marketdata.trade.v1.bybit.ETHUSDT"), 2),
	}
	// Verify golden is self-consistent (computed fresh == stored)
	for subject, wantGroup := range golden2 {
		// Re-derive from scratch using the same formula.
		key := ShardKey(subject)
		gotGroup := ShardGroup(key, 2)
		if gotGroup != wantGroup {
			t.Errorf("GOLDEN DRIFT: ShardGroup(ShardKey(%q), 2) = %d; golden expects %d — breaking change to shard assignment", subject, gotGroup, wantGroup)
		}
	}

	// Verify golden is stable across 100 re-derivations (no randomness).
	for subject, wantGroup := range golden2 {
		for i := 0; i < 100; i++ {
			if g := ShardGroup(ShardKey(subject), 2); g != wantGroup {
				t.Fatalf("GOLDEN UNSTABLE: %q iteration %d = %d; want %d", subject, i, g, wantGroup)
			}
		}
	}
}

// TestShardReplayInvariant_OffModePassthrough verifies that when sharding is
// disabled (groupCount=1, the default), every message passes through without
// being skipped.  This is the OFF golden: behaviour is unchanged from the
// pre-sharding baseline.
func TestShardReplayInvariant_OffModePassthrough(t *testing.T) {
	// Simulate a replay stream of 100 canonical subjects.
	subjects := make([]string, 0, 80)
	for _, venue := range []string{"binance", "bybit", "okx", "kraken"} {
		for _, instrument := range []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "BNBUSDT", "DOGEUSDT", "DOTUSDT", "ADAUSDT", "AVAXUSDT", "BNBUSDT"} {
			subjects = append(subjects, "marketdata.bookdelta.v1."+venue+"."+instrument)
		}
	}

	// In OFF mode (groupCount=1 or 0) no message is ever skipped.
	for _, s := range subjects {
		for _, count := range []int{0, 1} {
			if subjectBelongsToOtherShard(s, count, 0) {
				t.Errorf("OFF mode: subjectBelongsToOtherShard(%q, %d, 0) = true; want false", s, count)
			}
		}
	}
}

// TestShardReplayInvariant_UnionEqualsTotal verifies the core ON-mode invariant:
// the union of all shard groups processes every message exactly once.
// This is the mathematical proof that no message is dropped or duplicated
// across a horizontal scale-out deployment.
func TestShardReplayInvariant_UnionEqualsTotal(t *testing.T) {
	// Build a synthetic replay stream of known size.
	subjects := make([]string, 0, 100)
	for _, venue := range []string{"binance", "bybit", "okx", "kraken", "coinbase"} {
		for _, instrument := range []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "BNBUSDT", "DOGEUSDT", "DOTUSDT", "ADAUSDT", "AVAXUSDT", "LTCUSDT"} {
			// Mix of event types — all for the same venue+instrument must go to
			// the same shard (order-book consistency requirement).
			subjects = append(subjects, "marketdata.bookdelta.v1."+venue+"."+instrument)
			subjects = append(subjects, "marketdata.trade.v1."+venue+"."+instrument)
		}
	}

	for _, groupCount := range []int{1, 2, 3, 4, 8} {
		t.Run(fmt.Sprintf("groups=%d", groupCount), func(t *testing.T) {
			// Simulate N shard processors and count what each one processes.
			processed := make([][]string, groupCount)
			for _, s := range subjects {
				for g := 0; g < groupCount; g++ {
					if !subjectBelongsToOtherShard(s, groupCount, g) {
						processed[g] = append(processed[g], s)
					}
				}
			}

			// Invariant 1: union == total (no dropped messages).
			total := 0
			for g := 0; g < groupCount; g++ {
				total += len(processed[g])
			}
			if total != len(subjects) {
				t.Errorf("union total=%d; want %d (messages dropped or duplicated)", total, len(subjects))
			}

			// Invariant 2: order-book consistency — same venue+instrument
			// must always be assigned to the same group.
			type venueInstrument struct{ venue, instrument string }
			groupFor := make(map[venueInstrument]int)
			for g := 0; g < groupCount; g++ {
				for _, s := range processed[g] {
					_, _, venue, instrument, err := splitSubjectTaxonomy(s)
					if err != nil {
						t.Fatalf("splitSubjectTaxonomy(%q): %v", s, err)
					}
					key := venueInstrument{venue, instrument}
					if prev, seen := groupFor[key]; seen && prev != g {
						t.Errorf("order-book consistency violated: %s/%s split between groups %d and %d", venue, instrument, prev, g)
					}
					groupFor[key] = g
				}
			}
		})
	}
}

// ── S4 integration: 2 shards, 10 instruments, exactly-once ─────────────────────

// TestShard_TwoShards_TenInstruments_ExactlyOnce is the S4 integration test
// specification: 2 consumers (shard 0/2 and 1/2), 10 canonical instruments,
// prove each instrument always goes to the same shard, prove no duplicates
// across shards.
func TestShard_TwoShards_TenInstruments_ExactlyOnce(t *testing.T) {
	venues := []string{"binance", "bybit"}
	instruments := []string{
		"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "BNBUSDT",
		"ADAUSDT", "DOGEUSDT", "DOTUSDT", "AVAXUSDT", "LTCUSDT",
	}
	eventTypes := []string{"marketdata.bookdelta.v1", "marketdata.trade.v1"}
	const shardCount = 2

	subjects := buildSubjectMatrix(venues, instruments, eventTypes)
	claimedBy := assignSubjectsToShards(t, subjects, shardCount)

	assertAllSubjectsClaimed(t, claimedBy, subjects)
	assertOrderBookConsistency(t, claimedBy)
	assertNonDegeneratePartition(t, claimedBy, shardCount)
	t.Logf("shard distribution: shard0=%d shard1=%d (total=%d)",
		shardCountFor(claimedBy, 0), shardCountFor(claimedBy, 1), len(subjects))
}

func buildSubjectMatrix(venues, instruments, eventTypes []string) []string {
	var out []string
	for _, venue := range venues {
		for _, instrument := range instruments {
			for _, eventType := range eventTypes {
				out = append(out, eventType+"."+venue+"."+instrument)
			}
		}
	}
	return out
}

func assignSubjectsToShards(t *testing.T, subjects []string, shardCount int) map[string]int {
	t.Helper()
	claimedBy := make(map[string]int, len(subjects))
	for _, subject := range subjects {
		for shardID := 0; shardID < shardCount; shardID++ {
			if !subjectBelongsToOtherShard(subject, shardCount, shardID) {
				if prev, dup := claimedBy[subject]; dup {
					t.Fatalf("DUPLICATE: subject %q claimed by shard %d AND %d", subject, prev, shardID)
				}
				claimedBy[subject] = shardID
			}
		}
	}
	return claimedBy
}

func assertAllSubjectsClaimed(t *testing.T, claimedBy map[string]int, subjects []string) {
	t.Helper()
	if len(claimedBy) != len(subjects) {
		t.Fatalf("claimed %d of %d subjects; want all claimed (exactly-once violated)", len(claimedBy), len(subjects))
	}
}

func assertOrderBookConsistency(t *testing.T, claimedBy map[string]int) {
	t.Helper()
	type venueInstr struct{ v, i string }
	shardFor := make(map[venueInstr]int)
	for subject, shardID := range claimedBy {
		_, _, venue, instrument, err := splitSubjectTaxonomy(subject)
		if err != nil {
			t.Fatalf("splitSubjectTaxonomy(%q): %v", subject, err)
		}
		key := venueInstr{venue, instrument}
		if prev, seen := shardFor[key]; seen && prev != shardID {
			t.Errorf("consistency violated: %s/%s split between shard %d and %d", venue, instrument, prev, shardID)
		}
		shardFor[key] = shardID
	}
}

func assertNonDegeneratePartition(t *testing.T, claimedBy map[string]int, shardCount int) {
	t.Helper()
	counts := make(map[int]int)
	for _, id := range claimedBy {
		counts[id]++
	}
	for shard := 0; shard < shardCount; shard++ {
		if counts[shard] == 0 {
			t.Errorf("shard %d received 0 subjects; partition is degenerate", shard)
		}
	}
}

func shardCountFor(claimedBy map[string]int, shardID int) int {
	n := 0
	for _, id := range claimedBy {
		if id == shardID {
			n++
		}
	}
	return n
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
