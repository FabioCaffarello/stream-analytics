package contracts_test

import (
	"testing"

	"github.com/market-raccoon/internal/contracts"
	"github.com/market-raccoon/internal/shared/codec"
)

func TestRegisterLiquidityPayloadV1(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterLiquidityPayloadV1(reg); p != nil {
		t.Fatalf("RegisterLiquidityPayloadV1 failed: %s", p.Message)
	}
	for _, format := range []codec.Format{codec.FormatJSON, codec.FormatProto} {
		key := codec.SchemaKey{
			Type:    "liquidity.evidence",
			Version: 1,
			Format:  format,
		}
		if _, ok := reg.Decoder(key); !ok {
			t.Fatalf("decoder missing for format=%s", format)
		}
		if _, ok := reg.Encoder(key); !ok {
			t.Fatalf("encoder missing for format=%s", format)
		}
	}
}

func TestLiquidityEvidenceJSONRoundtrip(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterLiquidityPayloadV1(reg); p != nil {
		t.Fatalf("register failed: %s", p.Message)
	}
	key := codec.SchemaKey{Type: "liquidity.evidence", Version: 1, Format: codec.FormatJSON}
	enc, _ := reg.Encoder(key)
	dec, _ := reg.Decoder(key)
	original := contracts.LiquidityEvidenceV1{
		EvidenceType: "SWEEP",
		TsIngestMs:   1_709_500_000_000,
		Venue:        "BINANCE",
		Symbol:       "BTCUSDT",
		WindowMs:     1000,
		Severity:     "high",
		Confidence:   0.85,
		Metrics: []contracts.LiquidityEvidenceMetric{
			{Key: "depth_drop_pct", Value: 62.5},
			{Key: "level_drop", Value: 8},
		},
		Explain:  []string{"rapid level consumption detected on bid side"},
		Version:  1,
		StreamID: "BINANCE|BTCUSDT",
		Seq:      77,
		Watermark: contracts.LiquidityInputWatermark{
			SeqStart: 77,
			SeqEnd:   77,
		},
	}
	raw, p := enc.Encode(original)
	if p != nil {
		t.Fatalf("encode failed: %s", p.Message)
	}
	outAny, p := dec.Decode(raw)
	if p != nil {
		t.Fatalf("decode failed: %s", p.Message)
	}
	out, ok := outAny.(contracts.LiquidityEvidenceV1)
	if !ok {
		t.Fatalf("decoded type=%T want contracts.LiquidityEvidenceV1", outAny)
	}
	if out.EvidenceType != original.EvidenceType || out.Seq != original.Seq || out.Severity != original.Severity {
		t.Fatalf("decoded mismatch: got=%+v want=%+v", out, original)
	}
}

func TestLiquidityEvidenceProtoRoundtrip(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterLiquidityPayloadV1(reg); p != nil {
		t.Fatalf("register failed: %s", p.Message)
	}
	key := codec.SchemaKey{Type: "liquidity.evidence", Version: 1, Format: codec.FormatProto}
	enc, _ := reg.Encoder(key)
	dec, _ := reg.Decoder(key)

	original := contracts.LiquidityEvidenceV1{
		EvidenceType: "BOOK_IMBALANCE",
		TsIngestMs:   1_709_500_000_111,
		Venue:        "BYBIT",
		Symbol:       "ETHUSDT",
		WindowMs:     5000,
		Severity:     "critical",
		Confidence:   0.95,
		Metrics: []contracts.LiquidityEvidenceMetric{
			{Key: "consecutive", Value: 30},
			{Key: "imbalance", Value: 0.81},
		},
		Explain:  []string{"persistent bid/ask depth imbalance detected"},
		Version:  1,
		StreamID: "BYBIT|ETHUSDT",
		Seq:      99,
		Watermark: contracts.LiquidityInputWatermark{
			SeqStart: 70,
			SeqEnd:   99,
		},
	}
	raw, p := enc.Encode(original)
	if p != nil {
		t.Fatalf("encode failed: %s", p.Message)
	}
	outAny, p := dec.Decode(raw)
	if p != nil {
		t.Fatalf("decode failed: %s", p.Message)
	}
	out, ok := outAny.(contracts.LiquidityEvidenceV1)
	if !ok {
		t.Fatalf("decoded type=%T want contracts.LiquidityEvidenceV1", outAny)
	}
	if out.EvidenceType != original.EvidenceType {
		t.Fatalf("evidence_type=%s want=%s", out.EvidenceType, original.EvidenceType)
	}
	if out.Watermark.SeqStart != original.Watermark.SeqStart || out.Watermark.SeqEnd != original.Watermark.SeqEnd {
		t.Fatalf("watermark mismatch got=%+v want=%+v", out.Watermark, original.Watermark)
	}
}
