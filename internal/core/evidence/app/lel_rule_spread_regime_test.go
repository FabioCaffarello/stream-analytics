package app

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

func TestLELSpreadRegimeRuleFiresOnTransition(t *testing.T) {
	rule := NewLELSpreadRegimeRule(DefaultRuleConfig())
	stream := "BINANCE|BTCUSDT"
	for i := 1; i <= 10; i++ {
		rule.OnEvent(domain.LELEvent{
			Kind:      domain.LELEventKindSnapshot,
			Venue:     "binance",
			Symbol:    "BTC-USDT",
			StreamID:  stream,
			TsServer:  int64(i) * 1000,
			Seq:       int64(i),
			BestBid:   100,
			BestAsk:   100.04,
			SpreadBPS: 4,
		})
	}
	out := rule.OnEvent(domain.LELEvent{
		Kind:      domain.LELEventKindSnapshot,
		Venue:     "binance",
		Symbol:    "BTC-USDT",
		StreamID:  stream,
		TsServer:  11_000,
		Seq:       11,
		BestBid:   100,
		BestAsk:   101,
		SpreadBPS: 120,
	})
	if len(out) != 1 {
		t.Fatalf("emissions=%d want=1", len(out))
	}
	if out[0].EvidenceType != domain.LiquidityEvidenceTypeSpreadRegime {
		t.Fatalf("type=%s", out[0].EvidenceType)
	}
}
