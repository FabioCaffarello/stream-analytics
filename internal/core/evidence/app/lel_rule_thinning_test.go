package app

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func TestLELThinningRuleFiresOnZScoreAndDrop(t *testing.T) {
	rule := NewLELThinningRule(DefaultRuleConfig())
	stream := "BINANCE|BTCUSDT"
	for i := 1; i <= 10; i++ {
		rule.OnEvent(domain.LELEvent{
			Kind:      domain.LELEventKindSnapshot,
			Venue:     "binance",
			Symbol:    "BTC-USDT",
			StreamID:  stream,
			TsServer:  int64(i) * 1000,
			Seq:       int64(i),
			BidDepth:  500,
			AskDepth:  500,
			BidLevels: 20,
			AskLevels: 20,
		})
	}
	out := rule.OnEvent(domain.LELEvent{
		Kind:      domain.LELEventKindSnapshot,
		Venue:     "binance",
		Symbol:    "BTC-USDT",
		StreamID:  stream,
		TsServer:  11_000,
		Seq:       11,
		BidDepth:  100,
		AskDepth:  100,
		BidLevels: 8,
		AskLevels: 8,
	})
	if len(out) != 1 {
		t.Fatalf("emissions=%d want=1", len(out))
	}
	if out[0].EvidenceType != domain.LiquidityEvidenceTypeThinning {
		t.Fatalf("type=%s", out[0].EvidenceType)
	}
}
