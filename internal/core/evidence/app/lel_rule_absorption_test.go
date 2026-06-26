package app

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func TestLELAbsorptionRuleFiresOnVolumeSpikeWithTightSpread(t *testing.T) {
	cfg := DefaultRuleConfig()
	cfg.CooldownMs = 0
	rule := NewLELAbsorptionRule(cfg)
	stream := "BINANCE|BTCUSDT"
	_ = rule.OnEvent(domain.LELEvent{
		Kind:      domain.LELEventKindSnapshot,
		Venue:     "binance",
		Symbol:    "BTC-USDT",
		StreamID:  stream,
		TsServer:  1_000,
		Seq:       1,
		SpreadBPS: 5,
	})
	var out []domain.LiquidityEvidence
	for i := 2; i <= 12; i++ {
		out = append(out, rule.OnEvent(domain.LELEvent{
			Kind:          domain.LELEventKindTape,
			Venue:         "binance",
			Symbol:        "BTC-USDT",
			StreamID:      stream,
			TsServer:      int64(i) * 1000,
			Seq:           int64(i),
			TotalVolume:   10,
			WindowStartTs: int64(i-1) * 1000,
			WindowEndTs:   int64(i) * 1000,
		})...)
	}
	out = append(out, rule.OnEvent(domain.LELEvent{
		Kind:          domain.LELEventKindTape,
		Venue:         "binance",
		Symbol:        "BTC-USDT",
		StreamID:      stream,
		TsServer:      13_000,
		Seq:           13,
		TotalVolume:   250,
		WindowStartTs: 12_000,
		WindowEndTs:   13_000,
	})...)
	if len(out) == 0 {
		t.Fatal("expected at least one emission")
	}
	if out[0].EvidenceType != domain.LiquidityEvidenceTypeAbsorption {
		t.Fatalf("type=%s", out[0].EvidenceType)
	}
}
