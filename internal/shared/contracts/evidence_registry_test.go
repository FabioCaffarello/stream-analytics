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
		Kind:        evidencedomain.SpreadExplosion,
		TsServer:    1709500000000,
		Venue:       "binance",
		Symbol:      "BTC-USDT",
		Severity:    evidencedomain.SeverityHigh,
		Confidence:  0.85,
		Features:    []string{"spread_bps", "z_score"},
		FeatureVals: []float64{45.2, 3.7},
		Reason:      "spread z-score exceeded threshold",
		SeqTrigger:  42,
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
	if ev.Kind != original.Kind {
		t.Errorf("Kind = %s, want %s", ev.Kind, original.Kind)
	}
	if ev.Confidence != original.Confidence {
		t.Errorf("Confidence = %f, want %f", ev.Confidence, original.Confidence)
	}
	if len(ev.Features) != len(original.Features) {
		t.Errorf("Features length = %d, want %d", len(ev.Features), len(original.Features))
	}
}

func TestEvidenceProtoRoundtrip(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterEvidencePayloadV1(reg); p != nil {
		t.Fatalf("register failed: %s", p.Message)
	}

	original := evidencedomain.EvidenceEvent{
		Kind:        evidencedomain.Absorption,
		TsServer:    1709500000000,
		Venue:       "coinbase",
		Symbol:      "ETH-USD",
		Severity:    evidencedomain.SeverityCritical,
		Confidence:  0.95,
		Features:    []string{"volume_ratio", "cum_volume"},
		FeatureVals: []float64{8.5, 150000},
		Reason:      "large volume absorbed",
		SeqTrigger:  99,
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
	if ev.Kind != original.Kind {
		t.Errorf("Kind = %s, want %s", ev.Kind, original.Kind)
	}
	if ev.Severity != original.Severity {
		t.Errorf("Severity = %s, want %s", ev.Severity, original.Severity)
	}
	if ev.SeqTrigger != original.SeqTrigger {
		t.Errorf("SeqTrigger = %d, want %d", ev.SeqTrigger, original.SeqTrigger)
	}
}
