package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func newCandle(t *testing.T) *domain.CandleV1 {
	t.Helper()
	c, p := domain.NewCandleV1("BINANCE", "BTCUSDT", "1m", 60_000)
	if p != nil {
		t.Fatalf("NewCandleV1: %v", p)
	}
	return c
}

func TestCandleV1_NewValidation(t *testing.T) {
	if _, p := domain.NewCandleV1("", "BTCUSDT", "1m", 1); p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected validation failure for empty venue, got=%v", p)
	}
	if _, p := domain.NewCandleV1("BINANCE", "BTCUSDT", "2m", 1); p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected validation failure for invalid timeframe, got=%v", p)
	}
}

func TestCandleV1_SubMinuteTimeframes(t *testing.T) {
	for _, tf := range []string{"1s", "5s"} {
		c, p := domain.NewCandleV1("BINANCE", "BTCUSDT", tf, 1_000)
		if p != nil {
			t.Fatalf("NewCandleV1(%s): %v", tf, p)
		}
		if c.Timeframe != tf {
			t.Fatalf("timeframe=%s want=%s", c.Timeframe, tf)
		}
	}
}

func TestCandleV1_ApplyTrade_OHLCV(t *testing.T) {
	c := newCandle(t)
	if p := c.ApplyTrade(100, 2, true, 1); p != nil {
		t.Fatalf("ApplyTrade #1: %v", p)
	}
	if p := c.ApplyTrade(101, 1, false, 2); p != nil {
		t.Fatalf("ApplyTrade #2: %v", p)
	}
	if p := c.ApplyTrade(99, 4, true, 3); p != nil {
		t.Fatalf("ApplyTrade #3: %v", p)
	}

	if c.Open != 100 {
		t.Fatalf("open=%v want=100", c.Open)
	}
	if c.High != 101 {
		t.Fatalf("high=%v want=101", c.High)
	}
	if c.Low != 99 {
		t.Fatalf("low=%v want=99", c.Low)
	}
	if c.ClosePrice != 99 {
		t.Fatalf("close=%v want=99", c.ClosePrice)
	}
	if c.Volume != 7 {
		t.Fatalf("volume=%v want=7", c.Volume)
	}
	if c.BuyVolume != 6 || c.SellVolume != 1 {
		t.Fatalf("buy/sell volume=(%v,%v) want=(6,1)", c.BuyVolume, c.SellVolume)
	}
}

func TestCandleV1_Close_Immutability(t *testing.T) {
	c := newCandle(t)
	if p := c.ApplyTrade(100, 1, true, 1); p != nil {
		t.Fatalf("ApplyTrade: %v", p)
	}
	if p := c.Close(120_000); p != nil {
		t.Fatalf("Close: %v", p)
	}
	if p := c.ApplyTrade(101, 1, false, 2); p == nil || p.Code != problem.Conflict {
		t.Fatalf("expected conflict after close, got=%v", p)
	}
}

func TestCandleV1_VolumeInvariant(t *testing.T) {
	c := newCandle(t)
	trades := []struct {
		price float64
		qty   float64
		isBuy bool
		seq   int64
	}{
		{100.1, 1.25, true, 1},
		{100.2, 0.75, false, 2},
		{100.0, 3.50, true, 3},
	}
	for i := range trades {
		tr := trades[i]
		if p := c.ApplyTrade(tr.price, tr.qty, tr.isBuy, tr.seq); p != nil {
			t.Fatalf("ApplyTrade[%d]: %v", i, p)
		}
		if c.Volume != c.BuyVolume+c.SellVolume {
			t.Fatalf("volume invariant broken at %d: volume=%v buy+sell=%v", i, c.Volume, c.BuyVolume+c.SellVolume)
		}
	}
}

func TestCandleV1_HighLowInvariant(t *testing.T) {
	c := newCandle(t)
	if p := c.ApplyTrade(101, 1, true, 1); p != nil {
		t.Fatalf("ApplyTrade #1: %v", p)
	}
	if p := c.ApplyTrade(103, 1, false, 2); p != nil {
		t.Fatalf("ApplyTrade #2: %v", p)
	}
	if p := c.ApplyTrade(99, 1, false, 3); p != nil {
		t.Fatalf("ApplyTrade #3: %v", p)
	}
	if c.High < c.Open || c.High < c.ClosePrice || c.High < c.Low {
		t.Fatalf("high invariant broken: open=%v high=%v low=%v close=%v", c.Open, c.High, c.Low, c.ClosePrice)
	}
	if c.Low > c.Open || c.Low > c.ClosePrice || c.Low > c.High {
		t.Fatalf("low invariant broken: open=%v high=%v low=%v close=%v", c.Open, c.High, c.Low, c.ClosePrice)
	}
	if p := c.Validate(); p != nil {
		t.Fatalf("Validate: %v", p)
	}
}

func TestCandleV1_Deterministic(t *testing.T) {
	seq := []struct {
		price float64
		qty   float64
		isBuy bool
		seq   int64
	}{
		{100.11, 0.25, true, 1},
		{99.99, 0.50, false, 2},
		{101.01, 0.40, true, 3},
		{100.50, 0.10, false, 4},
	}

	left := newCandle(t)
	right := newCandle(t)
	for i := range seq {
		tr := seq[i]
		if p := left.ApplyTrade(tr.price, tr.qty, tr.isBuy, tr.seq); p != nil {
			t.Fatalf("left ApplyTrade[%d]: %v", i, p)
		}
		if p := right.ApplyTrade(tr.price, tr.qty, tr.isBuy, tr.seq); p != nil {
			t.Fatalf("right ApplyTrade[%d]: %v", i, p)
		}
	}

	if left.Open != right.Open || left.High != right.High || left.Low != right.Low || left.ClosePrice != right.ClosePrice {
		t.Fatalf("determinism failed for OHLC: left=%+v right=%+v", left, right)
	}
	if left.Volume != right.Volume || left.BuyVolume != right.BuyVolume || left.SellVolume != right.SellVolume {
		t.Fatalf("determinism failed for volume: left=%+v right=%+v", left, right)
	}
	if left.TradeCount != right.TradeCount || left.SeqFirst != right.SeqFirst || left.SeqLast != right.SeqLast {
		t.Fatalf("determinism failed for sequence fields: left=%+v right=%+v", left, right)
	}
}
