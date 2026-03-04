package signal

import (
	"bytes"
	"testing"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
)

func TestSignalRulesV0_Fixtures(t *testing.T) {
	t.Parallel()
	key := mustStreamKey(t, "binance", "BTC-USDT", marketmodel.ChannelEvidence)
	baseCfg := EngineConfig{
		Store: StateStoreConfig{
			PerStreamWindow:    16,
			PerTenantStreamCap: 16,
			GlobalStreamCap:    64,
			TTLMillis:          10_000,
			DedupWindowMillis:  1,
		},
		Rules: DefaultRulesConfig(),
	}

	t.Run("RegimeChange", func(t *testing.T) {
		rule := RegimeChangeRule{cfg: RegimeChangeConfig{WindowMs: 1000, MinBurst: 3, MinDistinctTypes: 2, RuleVersion: "v0"}}
		engine := NewSignalEngine(baseCfg, nil, rule)
		inputs := []evidencedomain.EvidenceEvent{
			makeEvidence("spread_explosion", 1000, 1, "high", 0.8),
			makeEvidence("liquidity_thinning", 1100, 2, "high", 0.8),
			makeEvidence("persistent_imbalance", 1200, 3, "high", 0.8),
		}
		out := runEvidence(t, engine, key, inputs)
		if len(out) != 1 || out[0].Event.Type != "regime_change" {
			t.Fatalf("expected regime_change, got %+v", out)
		}
	})

	t.Run("LiquidityCollapse", func(t *testing.T) {
		rule := LiquidityCollapseRule{cfg: LiquidityCollapseConfig{WindowMs: 2000, MinSpreadEvents: 1, MinThinningEvents: 1, RuleVersion: "v0"}}
		engine := NewSignalEngine(baseCfg, nil, rule)
		inputs := []evidencedomain.EvidenceEvent{
			makeEvidence("spread_explosion", 1000, 1, "high", 0.9),
			makeEvidence("liquidity_thinning", 1500, 2, "high", 0.9),
		}
		out := runEvidence(t, engine, key, inputs)
		if len(out) != 1 || out[0].Event.Type != "liquidity_collapse" {
			t.Fatalf("expected liquidity_collapse, got %+v", out)
		}
	})

	t.Run("PersistentImbalanceSignal", func(t *testing.T) {
		rule := PersistentImbalanceRule{cfg: PersistentImbalanceConfig{WindowMs: 2000, MinImbalanceEvents: 2, RequireAbsorptionHit: true, RuleVersion: "v0"}}
		engine := NewSignalEngine(baseCfg, nil, rule)
		inputs := []evidencedomain.EvidenceEvent{
			makeEvidence("persistent_imbalance", 1000, 1, "medium", 0.8),
			makeEvidence("persistent_imbalance", 1200, 2, "medium", 0.8),
			makeEvidence("absorption", 1300, 3, "high", 0.9),
		}
		out := runEvidence(t, engine, key, inputs)
		if len(out) != 1 || out[0].Event.Type != "persistent_imbalance_signal" {
			t.Fatalf("expected persistent_imbalance_signal, got %+v", out)
		}
	})

	t.Run("VenueDivergenceStub", func(t *testing.T) {
		rule := VenueDivergenceRule{cfg: VenueDivergenceConfig{Enabled: false, RuleVersion: "v0", AggregatorCap: 0}}
		engine := NewSignalEngine(baseCfg, nil, rule)
		inputs := []evidencedomain.EvidenceEvent{
			makeEvidence("spread_explosion", 1000, 1, "high", 0.8),
		}
		out := runEvidence(t, engine, key, inputs)
		if len(out) != 0 {
			t.Fatalf("expected no emission for disabled venue divergence, got %+v", out)
		}
	})
}

func TestSignalEngine_ReplayDeterminism_ByteIdentical(t *testing.T) {
	t.Parallel()
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry failed: %v", p)
	}
	cfg := EngineConfig{
		Store: StateStoreConfig{
			PerStreamWindow:    16,
			PerTenantStreamCap: 16,
			GlobalStreamCap:    64,
			TTLMillis:          10_000,
			DedupWindowMillis:  1,
		},
		Rules: DefaultRulesConfig(),
	}
	key := mustStreamKey(t, "binance", "ETH-USDT", marketmodel.ChannelEvidence)
	sequence := []evidencedomain.EvidenceEvent{
		makeEvidence("spread_explosion", 1000, 1, "high", 0.8),
		makeEvidence("liquidity_thinning", 1100, 2, "high", 0.9),
		makeEvidence("persistent_imbalance", 1200, 3, "medium", 0.7),
		makeEvidence("absorption", 1300, 4, "high", 0.9),
	}

	run := func() [][]byte {
		engine := NewSignalEngine(cfg, nil, BuildV0Rules(cfg.Rules)...)
		emitted := runEvidence(t, engine, key, sequence)
		out := make([][]byte, 0, len(emitted))
		for i := range emitted {
			wire, p := codec.EncodePayload(EventType, EventVersion, envelope.ContentTypeProto, emitted[i].Event)
			if p != nil {
				t.Fatalf("encode signal proto payload: %v", p)
			}
			out = append(out, wire)
		}
		return out
	}

	first := run()
	second := run()
	if len(first) != len(second) {
		t.Fatalf("emission count mismatch %d != %d", len(first), len(second))
	}
	for i := range first {
		if !bytes.Equal(first[i], second[i]) {
			t.Fatalf("signal bytes differ at index %d", i)
		}
	}
}

func runEvidence(t *testing.T, engine *SignalEngine, key marketmodel.StreamKey, events []evidencedomain.EvidenceEvent) []Emission {
	t.Helper()
	out := make([]Emission, 0)
	for i := range events {
		emissions, _, _, _, p := engine.OnEvidenceEvent(key, "tenant-a", events[i])
		if p != nil {
			t.Fatalf("OnEvidenceEvent failed: %v", p)
		}
		out = append(out, emissions...)
	}
	return out
}

func makeEvidence(kind string, ts, seq int64, severity string, confidence float64) evidencedomain.EvidenceEvent {
	return evidencedomain.EvidenceEvent{
		Type:       evidencedomain.EvidenceType(kind),
		TsServer:   ts,
		Venue:      "binance",
		Symbol:     "BTC-USDT",
		StreamID:   "binance/BTC-USDT/evidence",
		Seq:        seq,
		Severity:   evidencedomain.Severity(severity),
		Confidence: confidence,
		Features: []evidencedomain.EvidenceFeature{
			{Key: "f1", Value: 1},
		},
		Explanation: "fixture",
		RuleVersion: "v0",
		InputWatermark: evidencedomain.InputWatermark{
			SeqStart: seq,
			SeqEnd:   seq,
		},
	}
}

func mustStreamKey(t *testing.T, venue, symbol string, channel marketmodel.Channel) marketmodel.StreamKey {
	t.Helper()
	key, p := marketmodel.NewStreamKey(venue, symbol, channel)
	if p != nil {
		t.Fatalf("NewStreamKey failed: %v", p)
	}
	return key
}
