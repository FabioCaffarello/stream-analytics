package signal

import (
	"testing"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
)

func TestSignalStateStore_BoundedEvictions(t *testing.T) {
	t.Parallel()
	store := NewSignalStateStore(StateStoreConfig{
		PerStreamWindow:    4,
		PerTenantStreamCap: 1,
		GlobalStreamCap:    2,
		TTLMillis:          1000,
		DedupWindowMillis:  100,
	})

	k1 := mustStreamKey(t, "binance", "BTC-USDT", marketmodel.ChannelEvidence)
	k2 := mustStreamKey(t, "binance", "ETH-USDT", marketmodel.ChannelEvidence)
	k3 := mustStreamKey(t, "bybit", "SOL-USDT", marketmodel.ChannelEvidence)

	if ev, p := store.ObserveMarket(MarketObservation{Key: k1, Tenant: "tenant-a", TsServer: 100, Seq: 1}); p != nil || len(ev) != 0 {
		t.Fatalf("unexpected err=%v evictions=%v", p, ev)
	}
	if ev, p := store.ObserveMarket(MarketObservation{Key: k2, Tenant: "tenant-a", TsServer: 110, Seq: 2}); p != nil || !containsReason(ev, EvictionReasonTenant) {
		t.Fatalf("expected tenant eviction, err=%v evictions=%v", p, ev)
	}
	if entries := store.StreamEntries(); entries != 1 {
		t.Fatalf("entries=%d want=1", entries)
	}
	if ev, p := store.ObserveMarket(MarketObservation{Key: k3, Tenant: "tenant-b", TsServer: 120, Seq: 3}); p != nil || len(ev) != 0 {
		t.Fatalf("unexpected err=%v evictions=%v", p, ev)
	}
	if entries := store.StreamEntries(); entries != 2 {
		t.Fatalf("entries=%d want=2", entries)
	}
}

func TestSignalStateStore_TTLEviction_NoSleep(t *testing.T) {
	t.Parallel()
	store := NewSignalStateStore(StateStoreConfig{
		PerStreamWindow:    4,
		PerTenantStreamCap: 4,
		GlobalStreamCap:    4,
		TTLMillis:          50,
		DedupWindowMillis:  50,
	})
	k1 := mustStreamKey(t, "binance", "BTC-USDT", marketmodel.ChannelEvidence)
	k2 := mustStreamKey(t, "binance", "ETH-USDT", marketmodel.ChannelEvidence)

	if ev, p := store.ObserveMarket(MarketObservation{Key: k1, Tenant: "tenant-a", TsServer: 10, Seq: 1}); p != nil || len(ev) != 0 {
		t.Fatalf("unexpected err=%v evictions=%v", p, ev)
	}
	ev, p := store.ObserveMarket(MarketObservation{Key: k2, Tenant: "tenant-a", TsServer: 200, Seq: 2})
	if p != nil {
		t.Fatalf("observe market failed: %v", p)
	}
	if !containsReason(ev, EvictionReasonTTL) {
		t.Fatalf("expected ttl eviction, got %v", ev)
	}
}

func TestSignalStateStore_PerStreamRingFixed(t *testing.T) {
	t.Parallel()
	store := NewSignalStateStore(StateStoreConfig{
		PerStreamWindow:    2,
		PerTenantStreamCap: 4,
		GlobalStreamCap:    4,
		TTLMillis:          1000,
		DedupWindowMillis:  100,
	})
	k := mustStreamKey(t, "binance", "BTC-USDT", marketmodel.ChannelEvidence)

	for i := int64(1); i <= 3; i++ {
		snapshot, accepted, _, p := store.ObserveEvidence(k, "tenant-a", evidencedomain.EvidenceEvent{
			Type:           evidencedomain.SpreadExplosion,
			TsServer:       i * 10,
			Venue:          "binance",
			Symbol:         "BTC-USDT",
			StreamID:       "binance/BTC-USDT/evidence",
			Seq:            i,
			Severity:       evidencedomain.SeverityHigh,
			Confidence:     0.8,
			Features:       []evidencedomain.EvidenceFeature{{Key: "f", Value: 1}},
			Explanation:    "x",
			RuleVersion:    "v0",
			InputWatermark: evidencedomain.InputWatermark{SeqStart: i, SeqEnd: i},
		})
		if p != nil {
			t.Fatalf("ObserveEvidence failed: %v", p)
		}
		if !accepted {
			t.Fatal("expected accepted=true for monotonic evidence")
		}
		if i == 3 {
			if got := len(snapshot.EvidenceHistory); got != 2 {
				t.Fatalf("history len=%d want=2", got)
			}
			if snapshot.EvidenceHistory[0].Seq != 2 || snapshot.EvidenceHistory[1].Seq != 3 {
				t.Fatalf("unexpected ring content: %+v", snapshot.EvidenceHistory)
			}
		}
	}
}

func TestSignalStateStore_ObserveEvidence_IgnoresReplayBySeq(t *testing.T) {
	t.Parallel()
	store := NewSignalStateStore(StateStoreConfig{
		PerStreamWindow:    4,
		PerTenantStreamCap: 4,
		GlobalStreamCap:    4,
		TTLMillis:          1000,
		DedupWindowMillis:  100,
	})
	k := mustStreamKey(t, "binance", "BTC-USDT", marketmodel.ChannelEvidence)
	base := evidencedomain.EvidenceEvent{
		Type:           evidencedomain.SpreadExplosion,
		Venue:          "binance",
		Symbol:         "BTC-USDT",
		StreamID:       "binance/BTC-USDT/evidence",
		Severity:       evidencedomain.SeverityHigh,
		Confidence:     0.8,
		Features:       []evidencedomain.EvidenceFeature{{Key: "f", Value: 1}},
		Explanation:    "x",
		RuleVersion:    "v0",
		InputWatermark: evidencedomain.InputWatermark{SeqStart: 1, SeqEnd: 1},
	}

	first := base
	first.TsServer = 100
	first.Seq = 10
	snapshot, accepted, _, p := store.ObserveEvidence(k, "tenant-a", first)
	if p != nil {
		t.Fatalf("first ObserveEvidence failed: %v", p)
	}
	if !accepted {
		t.Fatal("expected first evidence accepted")
	}
	if got := len(snapshot.EvidenceHistory); got != 1 {
		t.Fatalf("history len=%d want=1", got)
	}

	replay := base
	replay.TsServer = 120
	replay.Seq = 10
	snapshot2, accepted2, _, p := store.ObserveEvidence(k, "tenant-a", replay)
	if p != nil {
		t.Fatalf("replay ObserveEvidence failed: %v", p)
	}
	if accepted2 {
		t.Fatal("expected replay evidence to be ignored")
	}
	if got := len(snapshot2.EvidenceHistory); got != 1 {
		t.Fatalf("history len after replay=%d want=1", got)
	}
	if got, want := snapshot2.LastSeq, int64(10); got != want {
		t.Fatalf("last seq=%d want=%d", got, want)
	}
}

func TestSignalStateStore_TenantRateLimitPerMinute(t *testing.T) {
	t.Parallel()
	store := NewSignalStateStore(StateStoreConfig{
		PerStreamWindow:    4,
		PerTenantStreamCap: 4,
		GlobalStreamCap:    4,
		TTLMillis:          1000,
		DedupWindowMillis:  100,
		TenantRateLimitMin: 1,
	})

	if ok := store.AllowTenantEmission("tenant-a", 60_000); !ok {
		t.Fatal("first tenant emission should be allowed")
	}
	if ok := store.AllowTenantEmission("tenant-a", 60_500); ok {
		t.Fatal("second tenant emission in same minute should be rate-limited")
	}
	if ok := store.AllowTenantEmission("tenant-a", 120_000); !ok {
		t.Fatal("tenant emission should reset in next minute")
	}
	if ok := store.AllowTenantEmission("tenant-b", 60_700); !ok {
		t.Fatal("different tenant should have isolated budget")
	}
}

func containsReason(in []EvictionReason, target EvictionReason) bool {
	for i := range in {
		if in[i] == target {
			return true
		}
	}
	return false
}
