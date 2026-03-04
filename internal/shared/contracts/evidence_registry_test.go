package contracts_test

import (
	"testing"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
)

func TestRegisterEvidencePayloadV1(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterEvidencePayloadV1(reg); p != nil {
		t.Fatalf("RegisterEvidencePayloadV1 failed: %s", p.Message)
	}
	// Verify JSON codec exists
	key := codec.SchemaKey{
		Type:    evidencedomain.MicrostructureEvidenceType,
		Version: 1,
		Format:  codec.FormatJSON,
	}
	if _, ok := reg.Decoder(key); !ok {
		t.Error("JSON decoder not registered")
	}
	if _, ok := reg.Encoder(key); !ok {
		t.Error("JSON encoder not registered")
	}
	regimeKey := codec.SchemaKey{
		Type:    evidencedomain.RegimeEvidenceType,
		Version: 1,
		Format:  codec.FormatJSON,
	}
	if _, ok := reg.Decoder(regimeKey); !ok {
		t.Error("regime JSON decoder not registered")
	}
	if _, ok := reg.Encoder(regimeKey); !ok {
		t.Error("regime JSON encoder not registered")
	}
	// Verify Proto codec exists
	keyProto := codec.SchemaKey{
		Type:    evidencedomain.MicrostructureEvidenceType,
		Version: 1,
		Format:  codec.FormatProto,
	}
	if _, ok := reg.Decoder(keyProto); !ok {
		t.Error("Proto decoder not registered")
	}
}

func TestRegimeJSONRoundtrip(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterEvidencePayloadV1(reg); p != nil {
		t.Fatalf("register failed: %s", p.Message)
	}

	original := evidencedomain.RegimeSignal{
		Venue:       "binance",
		Instrument:  "BTC-USDT",
		Timeframe:   "1m",
		Kind:        evidencedomain.RegimeTrending,
		Strength:    0.82,
		Confidence:  0.91,
		WindowStart: 1709500000000,
		WindowEnd:   1709500060000,
		Features: []evidencedomain.FeaturePair{
			{Name: "slope_ratio", Value: 0.0032},
		},
	}

	key := codec.SchemaKey{
		Type:    evidencedomain.RegimeEvidenceType,
		Version: 1,
		Format:  codec.FormatJSON,
	}
	enc, ok := reg.Encoder(key)
	if !ok {
		t.Fatal("regime encoder not found")
	}
	data, p := enc.Encode(original)
	if p != nil {
		t.Fatalf("encode failed: %s", p.Message)
	}

	dec, ok := reg.Decoder(key)
	if !ok {
		t.Fatal("regime decoder not found")
	}
	decoded, p := dec.Decode(data)
	if p != nil {
		t.Fatalf("decode failed: %s", p.Message)
	}

	got, ok := decoded.(evidencedomain.RegimeSignal)
	if !ok {
		t.Fatalf("decoded type = %T, want RegimeSignal", decoded)
	}
	if got.Kind != original.Kind {
		t.Fatalf("kind = %s, want %s", got.Kind, original.Kind)
	}
	if got.WindowEnd != original.WindowEnd {
		t.Fatalf("window_end = %d, want %d", got.WindowEnd, original.WindowEnd)
	}
}

func TestEvidenceJSONRoundtrip(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterEvidencePayloadV1(reg); p != nil {
		t.Fatalf("register failed: %s", p.Message)
	}

	original := evidencedomain.EvidenceEvent{
		Type:       evidencedomain.SpreadExplosion,
		TsServer:   1709500000000,
		Venue:      "binance",
		Symbol:     "BTC-USDT",
		StreamID:   "binance/BTC-USDT/book_delta",
		Seq:        42,
		Severity:   evidencedomain.SeverityHigh,
		Confidence: 0.85,
		Features: []evidencedomain.EvidenceFeature{
			{Key: "spread_bps", Value: 45.2},
			{Key: "z_score", Value: 3.7},
		},
		Explanation: "spread z-score exceeded threshold",
		RuleVersion: evidencedomain.RuleVersionV0,
		InputWatermark: evidencedomain.InputWatermark{
			SeqStart: 40,
			SeqEnd:   42,
		},
	}

	key := codec.SchemaKey{
		Type:    evidencedomain.MicrostructureEvidenceType,
		Version: 1,
		Format:  codec.FormatJSON,
	}
	enc, ok := reg.Encoder(key)
	if !ok {
		t.Fatal("encoder not found")
	}
	data, p := enc.Encode(original)
	if p != nil {
		t.Fatalf("encode failed: %s", p.Message)
	}

	dec, ok := reg.Decoder(key)
	if !ok {
		t.Fatal("decoder not found")
	}
	decoded, p := dec.Decode(data)
	if p != nil {
		t.Fatalf("decode failed: %s", p.Message)
	}

	ev, ok := decoded.(evidencedomain.EvidenceEvent)
	if !ok {
		t.Fatalf("decoded type = %T, want EvidenceEvent", decoded)
	}
	if ev.Type != original.Type {
		t.Errorf("Type = %s, want %s", ev.Type, original.Type)
	}
	if ev.Confidence != original.Confidence {
		t.Errorf("Confidence = %f, want %f", ev.Confidence, original.Confidence)
	}
	if len(ev.Features) != len(original.Features) {
		t.Errorf("Features length = %d, want %d", len(ev.Features), len(original.Features))
	}
	if ev.Explanation != original.Explanation {
		t.Errorf("Explanation = %q, want %q", ev.Explanation, original.Explanation)
	}
	if ev.InputWatermark.SeqEnd != original.InputWatermark.SeqEnd {
		t.Errorf("InputWatermark.SeqEnd = %d, want %d", ev.InputWatermark.SeqEnd, original.InputWatermark.SeqEnd)
	}
}

func TestEvidenceProtoRoundtrip(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterEvidencePayloadV1(reg); p != nil {
		t.Fatalf("register failed: %s", p.Message)
	}

	original := evidencedomain.EvidenceEvent{
		Type:       evidencedomain.Absorption,
		TsServer:   1709500000000,
		Venue:      "coinbase",
		Symbol:     "ETH-USD",
		StreamID:   "coinbase/ETH-USD/trade",
		Seq:        99,
		Severity:   evidencedomain.SeverityCritical,
		Confidence: 0.95,
		Features: []evidencedomain.EvidenceFeature{
			{Key: "cum_volume", Value: 150000},
			{Key: "volume_ratio", Value: 8.5},
		},
		Explanation: "large volume absorbed",
		RuleVersion: evidencedomain.RuleVersionV0,
		InputWatermark: evidencedomain.InputWatermark{
			SeqStart: 97,
			SeqEnd:   99,
		},
	}

	key := codec.SchemaKey{
		Type:    evidencedomain.MicrostructureEvidenceType,
		Version: 1,
		Format:  codec.FormatProto,
	}
	enc, ok := reg.Encoder(key)
	if !ok {
		t.Fatal("proto encoder not found")
	}
	data, p := enc.Encode(original)
	if p != nil {
		t.Fatalf("proto encode failed: %s", p.Message)
	}

	dec, ok := reg.Decoder(key)
	if !ok {
		t.Fatal("proto decoder not found")
	}
	decoded, p := dec.Decode(data)
	if p != nil {
		t.Fatalf("proto decode failed: %s", p.Message)
	}

	ev, ok := decoded.(evidencedomain.EvidenceEvent)
	if !ok {
		t.Fatalf("decoded type = %T, want EvidenceEvent", decoded)
	}
	if ev.Type != original.Type {
		t.Errorf("Type = %s, want %s", ev.Type, original.Type)
	}
	if ev.Severity != original.Severity {
		t.Errorf("Severity = %s, want %s", ev.Severity, original.Severity)
	}
	if ev.Seq != original.Seq {
		t.Errorf("Seq = %d, want %d", ev.Seq, original.Seq)
	}
	if ev.RuleVersion != original.RuleVersion {
		t.Errorf("RuleVersion = %s, want %s", ev.RuleVersion, original.RuleVersion)
	}
}
