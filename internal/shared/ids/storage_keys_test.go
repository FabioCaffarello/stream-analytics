package ids_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/ids"
)

func TestAggregationSnapshotWriteKey_DeterministicAndCanonical(t *testing.T) {
	a := ids.AggregationSnapshotWriteKey("binance", "btc-usdt", 42, "src-key")
	b := ids.AggregationSnapshotWriteKey("BINANCE", "BTCUSDT", 42, "src-key")
	if a != b {
		t.Fatalf("write key mismatch for equivalent identities: %q vs %q", a, b)
	}
}

func TestAggregationSnapshotWriteKey_ChangesWithSeq(t *testing.T) {
	a := ids.AggregationSnapshotWriteKey("binance", "BTCUSDT", 41, "src-key")
	b := ids.AggregationSnapshotWriteKey("binance", "BTCUSDT", 42, "src-key")
	if a == b {
		t.Fatal("write key must change when seq changes")
	}
}

func TestAggregationSnapshotWriteKey_ChangesWithSourceIdempotency(t *testing.T) {
	a := ids.AggregationSnapshotWriteKey("binance", "BTCUSDT", 42, "src-a")
	b := ids.AggregationSnapshotWriteKey("binance", "BTCUSDT", 42, "src-b")
	if a == b {
		t.Fatal("write key must change when source idempotency key changes")
	}
}
