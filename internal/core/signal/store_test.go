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
		snapshot, _, p := store.ObserveEvidence(k, "tenant-a", evidencedomain.EvidenceEvent{
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

func containsReason(in []EvictionReason, target EvictionReason) bool {
	for i := range in {
		if in[i] == target {
			return true
		}
	}
	return false
}
