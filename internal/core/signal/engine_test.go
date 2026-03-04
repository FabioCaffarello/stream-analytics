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

func TestSignalEngine_LELEvidence_RegimeChange(t *testing.T) {
	t.Parallel()
	cfg := testLELEngineConfig()
	key := mustStreamKey(t, "binance", "BTC-USDT", marketmodel.ChannelEvidence)
	engine := NewSignalEngine(cfg, nil, BuildV0Rules(cfg.Rules)...)

	events := []evidencedomain.EvidenceEvent{
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeSweep, 1000, 1, 0.80)),
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeThinning, 1500, 2, 0.82)),
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeSpreadRegime, 1900, 3, 0.88)),
	}

	out := runEvidence(t, engine, key, events)
	if !containsSignalType(out, "regime_change") {
		t.Fatalf("expected regime_change signal, got=%+v", out)
	}
	assertRuleVersionForType(t, out, "regime_change", "v1")
}

func TestSignalEngine_LELEvidence_LiquidityCollapse(t *testing.T) {
	t.Parallel()
	cfg := testLELEngineConfig()
	key := mustStreamKey(t, "binance", "BTC-USDT", marketmodel.ChannelEvidence)
	engine := NewSignalEngine(cfg, nil, BuildV0Rules(cfg.Rules)...)

	events := []evidencedomain.EvidenceEvent{
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeThinning, 2000, 10, 0.9)),
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeSpreadRegime, 2200, 11, 0.92)),
	}
	out := runEvidence(t, engine, key, events)
	if !containsSignalType(out, "liquidity_collapse") {
		t.Fatalf("expected liquidity_collapse signal, got=%+v", out)
	}
	assertRuleVersionForType(t, out, "liquidity_collapse", "v1")
}

func TestSignalEngine_LELEvidence_PersistentImbalance(t *testing.T) {
	t.Parallel()
	cfg := testLELEngineConfig()
	key := mustStreamKey(t, "binance", "BTC-USDT", marketmodel.ChannelEvidence)
	engine := NewSignalEngine(cfg, nil, BuildV0Rules(cfg.Rules)...)

	events := []evidencedomain.EvidenceEvent{
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeBookImbalance, 3000, 21, 0.78)),
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeBookImbalance, 3300, 22, 0.82)),
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeAbsorption, 3500, 23, 0.86)),
	}
	out := runEvidence(t, engine, key, events)
	if !containsSignalType(out, "persistent_imbalance_signal") {
		t.Fatalf("expected persistent_imbalance_signal, got=%+v", out)
	}
	assertRuleVersionForType(t, out, "persistent_imbalance_signal", "v1")
}

func TestSignalEngine_LELEvidence_DeterministicReplay(t *testing.T) {
	t.Parallel()
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		t.Fatalf("BootstrapPayloadCodecRegistry failed: %v", p)
	}
	cfg := testLELEngineConfig()
	key := mustStreamKey(t, "binance", "BTC-USDT", marketmodel.ChannelEvidence)
	sequence := []evidencedomain.EvidenceEvent{
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeSweep, 1000, 1, 0.81)),
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeThinning, 1100, 2, 0.84)),
		mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeSpreadRegime, 1200, 3, 0.88)),
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

func TestSignalEngine_LELEvidence_DedupReplaySuppressed(t *testing.T) {
	t.Parallel()
	cfg := testLELEngineConfig()
	key := mustStreamKey(t, "binance", "BTC-USDT", marketmodel.ChannelEvidence)
	engine := NewSignalEngine(cfg, nil, BuildV0Rules(cfg.Rules)...)

	first := mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeThinning, 5000, 31, 0.9))
	second := mustAdaptLEL(t, makeLELEvidence(evidencedomain.LiquidityEvidenceTypeSpreadRegime, 5300, 32, 0.93))
	replay := second

	out := runEvidence(t, engine, key, []evidencedomain.EvidenceEvent{first, second})
	if !containsSignalType(out, "liquidity_collapse") {
		t.Fatalf("expected liquidity_collapse before replay, got=%+v", out)
	}

	emissions, _, _, _, p := engine.OnEvidenceEvent(key, "tenant-a", replay)
	if p != nil {
		t.Fatalf("replay OnEvidenceEvent failed: %v", p)
	}
	if len(emissions) != 0 {
		t.Fatalf("expected replay duplicate suppression, got emissions=%+v", emissions)
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

func testLELEngineConfig() EngineConfig {
	return EngineConfig{
		Store: StateStoreConfig{
			PerStreamWindow:    64,
			PerTenantStreamCap: 64,
			GlobalStreamCap:    512,
			TTLMillis:          10_000,
			DedupWindowMillis:  5000,
		},
		Rules: DefaultRulesConfig(),
	}
}

func makeLELEvidence(
	typ evidencedomain.LiquidityEvidenceType,
	tsIngestMs, seq int64,
	confidence float64,
) evidencedomain.LiquidityEvidence {
	seqStart := seq - 1
	if seqStart <= 0 {
		seqStart = 1
	}
	return evidencedomain.LiquidityEvidence{
		EvidenceType: typ,
		TsIngestMs:   tsIngestMs,
		Venue:        "binance",
		Symbol:       "BTC-USDT",
		WindowMs:     3000,
		Severity:     evidencedomain.LiquidityEvidenceSeverityHigh,
		Confidence:   confidence,
		Metrics: []evidencedomain.LiquidityEvidenceMetric{
			{Key: "pressure", Value: 1.25},
			{Key: "spread_bps", Value: 0.7},
		},
		Explain:  []string{"lel evidence fixture"},
		Version:  evidencedomain.LiquidityEvidenceVersion,
		StreamID: "BINANCE|BTCUSDT",
		Seq:      seq,
		Watermark: evidencedomain.LiquidityInputWatermark{
			SeqStart: seqStart,
			SeqEnd:   seq,
		},
	}
}

func mustAdaptLEL(t *testing.T, in evidencedomain.LiquidityEvidence) evidencedomain.EvidenceEvent {
	t.Helper()
	out, p := LELToEvidenceEvent(in)
	if p != nil {
		t.Fatalf("LELToEvidenceEvent failed: %v", p)
	}
	return out
}

func containsSignalType(emissions []Emission, signalType string) bool {
	for i := range emissions {
		if emissions[i].Event.Type == signalType {
			return true
		}
	}
	return false
}

func assertRuleVersionForType(t *testing.T, emissions []Emission, signalType, expectedVersion string) {
	t.Helper()
	for i := range emissions {
		if emissions[i].Event.Type == signalType {
			if got := emissions[i].Event.RuleVersion; got != expectedVersion {
				t.Fatalf("signal %s rule_version=%q want=%q", signalType, got, expectedVersion)
			}
			return
		}
	}
	t.Fatalf("signal type %s not found in emissions", signalType)
}

func mustStreamKey(t *testing.T, venue, symbol string, channel marketmodel.Channel) marketmodel.StreamKey {
	t.Helper()
	key, p := marketmodel.NewStreamKey(venue, symbol, channel)
	if p != nil {
		t.Fatalf("NewStreamKey failed: %v", p)
	}
	return key
}
