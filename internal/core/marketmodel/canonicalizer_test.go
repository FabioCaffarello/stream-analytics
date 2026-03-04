package marketmodel

import (
	"testing"
	"time"

	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNormalizeBookDelta_DeterministicOrdering(t *testing.T) {
	adapter, p := NewStaticExchangeAdapter("binance", PrecisionRule{PriceDecimals: 2, SizeDecimals: 3}, nil)
	if p != nil {
		t.Fatalf("adapter: %v", p)
	}
	symbol, p := NewSymbol("BTC-USDT")
	if p != nil {
		t.Fatalf("symbol: %v", p)
	}

	out, p := NormalizeBookDelta(adapter, symbol, BookDelta{
		Bids: []Level{
			{Price: 100.125, Size: 1.5555},
			{Price: 101.999, Size: 1.2},
			{Price: 100.125, Size: 2.0},
		},
		Asks: []Level{
			{Price: 103.222, Size: 1.0},
			{Price: 102.444, Size: 1.0},
		},
		FirstID:   10,
		FinalID:   12,
		Timestamp: 1000,
	}, 1000)
	if p != nil {
		t.Fatalf("normalize: %v", p)
	}
	if len(out.Bids) != 2 {
		t.Fatalf("bids len=%d want=2", len(out.Bids))
	}
	if got, want := out.Bids[0].Price, 102.00; got != want {
		t.Fatalf("best bid=%v want=%v", got, want)
	}
	if got, want := out.Bids[1].Size, 2.0; got != want {
		t.Fatalf("collapsed bid size=%v want=%v", got, want)
	}
	if got, want := out.Asks[0].Price, 102.44; got != want {
		t.Fatalf("best ask=%v want=%v", got, want)
	}
}

func TestNormalizeTrade_PrecisionRounding(t *testing.T) {
	adapter, p := NewStaticExchangeAdapter("bybit", PrecisionRule{PriceDecimals: 2, SizeDecimals: 3}, nil)
	if p != nil {
		t.Fatalf("adapter: %v", p)
	}
	symbol, p := NewSymbol("BTC-USDT")
	if p != nil {
		t.Fatalf("symbol: %v", p)
	}
	out, p := NormalizeTrade(adapter, symbol, Trade{
		Price:     100.127,
		Size:      0.12355,
		Side:      "BUY",
		TradeID:   "t-1",
		Timestamp: 1710000000123,
	}, 1710000000123)
	if p != nil {
		t.Fatalf("normalize: %v", p)
	}
	if got, want := out.Price, 100.13; got != want {
		t.Fatalf("price=%v want=%v", got, want)
	}
	if got, want := out.Size, 0.124; got != want {
		t.Fatalf("size=%v want=%v", got, want)
	}
	if got, want := out.Side, "buy"; got != want {
		t.Fatalf("side=%q want=%q", got, want)
	}
}

func TestStateStore_SeqMonotonicPerStream(t *testing.T) {
	store := NewStateStore(StateStoreConfig{MaxEntries: 8, TTL: time.Hour, Now: time.Now})
	key, p := NewStreamKey("binance", "BTC-USDT", ChannelBookDelta)
	if p != nil {
		t.Fatalf("stream key: %v", p)
	}
	if p := store.UpsertSnapshot(key, 10, BookSnapshot{
		Bids:      []Level{{Price: 100, Size: 1}},
		Asks:      []Level{{Price: 101, Size: 1}},
		Timestamp: 1000,
	}); p != nil {
		t.Fatalf("upsert snapshot: %v", p)
	}
	if _, p := store.ApplyDelta(key, 10, BookDelta{
		Bids:      []Level{{Price: 100, Size: 2}},
		Asks:      nil,
		FirstID:   1,
		FinalID:   1,
		Timestamp: 1100,
	}, 1100); p == nil {
		t.Fatal("expected out-of-order error")
	}
}

func TestSnapshotDeltaReconstruction_Deterministic(t *testing.T) {
	storeA := NewStateStore(StateStoreConfig{MaxEntries: 8, TTL: time.Hour, Now: time.Now})
	storeB := NewStateStore(StateStoreConfig{MaxEntries: 8, TTL: time.Hour, Now: time.Now})
	key, p := NewStreamKey("binance", "BTC-USDT", ChannelBookDelta)
	if p != nil {
		t.Fatalf("stream key: %v", p)
	}
	snap := BookSnapshot{
		Bids:      []Level{{Price: 100, Size: 1}, {Price: 99, Size: 2}},
		Asks:      []Level{{Price: 101, Size: 1}},
		Timestamp: 1000,
	}
	if p := storeA.UpsertSnapshot(key, 1, snap); p != nil {
		t.Fatalf("storeA snapshot: %v", p)
	}
	if p := storeB.UpsertSnapshot(key, 1, snap); p != nil {
		t.Fatalf("storeB snapshot: %v", p)
	}
	delta := BookDelta{
		Bids:      []Level{{Price: 100, Size: 1.5}},
		Asks:      []Level{{Price: 101, Size: 0}},
		FirstID:   2,
		FinalID:   2,
		Timestamp: 1100,
	}
	a, p := storeA.ApplyDelta(key, 2, delta, 1100)
	if p != nil {
		t.Fatalf("storeA delta: %v", p)
	}
	b, p := storeB.ApplyDelta(key, 2, delta, 1100)
	if p != nil {
		t.Fatalf("storeB delta: %v", p)
	}
	if len(a.Bids) != len(b.Bids) || len(a.Asks) != len(b.Asks) {
		t.Fatalf("snapshot lengths diverged: A=%d/%d B=%d/%d", len(a.Bids), len(a.Asks), len(b.Bids), len(b.Asks))
	}
	if got, want := a.Bids[0].Size, b.Bids[0].Size; got != want {
		t.Fatalf("best bid size diverged: A=%v B=%v", got, want)
	}
}

func TestStateStore_BoundedEvictionAndMetric(t *testing.T) {
	before := testutil.ToFloat64(metrics.CanonicalStateEvictedTotal.WithLabelValues("capacity"))
	store := NewStateStore(StateStoreConfig{MaxEntries: 2, TTL: time.Hour, Now: time.Now})

	keys := []StreamKey{}
	for _, symbol := range []string{"BTC-USDT", "ETH-USDT", "SOL-USDT"} {
		k, p := NewStreamKey("binance", symbol, ChannelBookDelta)
		if p != nil {
			t.Fatalf("stream key: %v", p)
		}
		keys = append(keys, k)
	}
	for i, k := range keys {
		if p := store.UpsertSnapshot(k, Seq(i+1), BookSnapshot{
			Bids:      []Level{{Price: 100, Size: 1}},
			Asks:      []Level{{Price: 101, Size: 1}},
			Timestamp: 1000,
		}); p != nil {
			t.Fatalf("upsert snapshot[%d]: %v", i, p)
		}
	}
	if got := store.Entries(); got != 2 {
		t.Fatalf("entries=%d want=2", got)
	}
	after := testutil.ToFloat64(metrics.CanonicalStateEvictedTotal.WithLabelValues("capacity"))
	if after <= before {
		t.Fatalf("capacity eviction metric did not increase: before=%v after=%v", before, after)
	}
}
