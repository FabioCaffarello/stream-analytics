package app

import "testing"

func TestEvidenceStateStoreCapacityEvictionDeterministic(t *testing.T) {
	store := NewEvidenceStateStore(EvidenceStateStoreConfig{MaxEntries: 2, TTLMillis: 10_000})

	_ = store.Observe("b/eth/book", 1, 1000)
	_ = store.Observe("a/btc/book", 1, 1000)
	res := store.Observe("c/sol/book", 1, 2000)

	if len(res.Evictions) != 1 {
		t.Fatalf("evictions=%d want=1", len(res.Evictions))
	}
	if got, want := res.Evictions[0].StreamID, "a/btc/book"; got != want {
		t.Fatalf("victim=%q want=%q", got, want)
	}
	if got := store.Len(); got != 2 {
		t.Fatalf("len=%d want=2", got)
	}
}

func TestEvidenceStateStoreTTLEviction(t *testing.T) {
	store := NewEvidenceStateStore(EvidenceStateStoreConfig{MaxEntries: 8, TTLMillis: 500})
	_ = store.Observe("binance/BTC/book", 1, 1000)
	res := store.Observe("binance/ETH/book", 1, 1700)

	if len(res.Evictions) != 1 {
		t.Fatalf("evictions=%d want=1", len(res.Evictions))
	}
	if got, want := res.Evictions[0].Reason, "ttl"; got != want {
		t.Fatalf("reason=%q want=%q", got, want)
	}
}

func TestEvidenceStateStoreRejectsNonMonotonicSeq(t *testing.T) {
	store := NewEvidenceStateStore(EvidenceStateStoreConfig{MaxEntries: 8, TTLMillis: 5000})
	_ = store.Observe("binance/BTC/book", 10, 1000)
	res := store.Observe("binance/BTC/book", 10, 1100)
	if res.Accepted {
		t.Fatal("expected non_monotonic seq to be rejected")
	}
	if got, want := res.Reason, "non_monotonic_seq"; got != want {
		t.Fatalf("reason=%q want=%q", got, want)
	}
}
