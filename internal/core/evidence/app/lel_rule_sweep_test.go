package app

import (
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

func TestLELSweepRuleFiresOnLevelAndDepthDrop(t *testing.T) {
	cfg := DefaultRuleConfig()
	cfg.CooldownMs = 0
	rule := NewLELSweepRule(cfg)
	stream := "BINANCE|BTCUSDT"
	_ = rule.OnEvent(domain.LELEvent{
		Kind:      domain.LELEventKindSnapshot,
		Venue:     "binance",
		Symbol:    "BTC-USDT",
		StreamID:  stream,
		TsServer:  1000,
		Seq:       1,
		BidLevels: 20,
		AskLevels: 20,
		BidDepth:  1000,
		AskDepth:  1000,
	})
	out := rule.OnEvent(domain.LELEvent{
		Kind:      domain.LELEventKindSnapshot,
		Venue:     "binance",
		Symbol:    "BTC-USDT",
		StreamID:  stream,
		TsServer:  7000,
		Seq:       2,
		BidLevels: 12,
		AskLevels: 20,
		BidDepth:  300,
		AskDepth:  1000,
	})
	if len(out) != 1 {
		t.Fatalf("emissions=%d want=1", len(out))
	}
	if out[0].EvidenceType != domain.LiquidityEvidenceTypeSweep {
		t.Fatalf("type=%s", out[0].EvidenceType)
	}
}
