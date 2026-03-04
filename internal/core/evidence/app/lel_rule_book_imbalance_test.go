package app

import (
	"testing"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

func TestLELBookImbalanceRuleFiresAfterConsecutiveSnapshots(t *testing.T) {
	cfg := DefaultRuleConfig()
	cfg.CooldownMs = 0
	rule := NewLELBookImbalanceRule(cfg)
	var emittedAll []domain.LiquidityEvidence
	for i := 1; i <= 12; i++ {
		emitted := rule.OnEvent(domain.LELEvent{
			Kind:      domain.LELEventKindSnapshot,
			Venue:     "binance",
			Symbol:    "BTC-USDT",
			StreamID:  "BINANCE|BTCUSDT",
			TsServer:  int64(i) * 1000,
			Seq:       int64(i),
			BidDepth:  900,
			AskDepth:  100,
			BidLevels: 20,
			AskLevels: 20,
		})
		emittedAll = append(emittedAll, emitted...)
	}
	if len(emittedAll) == 0 {
		t.Fatal("expected at least one emission")
	}
	if emittedAll[0].EvidenceType != domain.LiquidityEvidenceTypeBookImbalance {
		t.Fatalf("type=%s", emittedAll[0].EvidenceType)
	}
}
