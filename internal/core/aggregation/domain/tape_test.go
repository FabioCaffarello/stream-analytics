package domain_test

import (
	"math"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
)

func TestTapeWindowV1_ApplyTrade_Accumulates(t *testing.T) {
	w, p := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 1000)
	if p != nil {
		t.Fatalf("NewTapeWindowV1: %v", p)
	}
	if p := w.ApplyTrade(100, 2, true, 1); p != nil {
		t.Fatalf("ApplyTrade buy: %v", p)
	}
	if p := w.ApplyTrade(101, 1.5, false, 2); p != nil {
		t.Fatalf("ApplyTrade sell: %v", p)
	}

	if got, want := w.TradeCount, int64(2); got != want {
		t.Fatalf("TradeCount=%d want=%d", got, want)
	}
	if got, want := w.BuyCount, int64(1); got != want {
		t.Fatalf("BuyCount=%d want=%d", got, want)
	}
	if got, want := w.SellCount, int64(1); got != want {
		t.Fatalf("SellCount=%d want=%d", got, want)
	}
	if math.Abs(w.BuyVolume-2) > 1e-9 {
		t.Fatalf("BuyVolume=%f want=2", w.BuyVolume)
	}
	if math.Abs(w.SellVolume-1.5) > 1e-9 {
		t.Fatalf("SellVolume=%f want=1.5", w.SellVolume)
	}
	if math.Abs(w.TotalVolume-3.5) > 1e-9 {
		t.Fatalf("TotalVolume=%f want=3.5", w.TotalVolume)
	}
	if math.Abs(w.BuyNotional-200) > 1e-9 {
		t.Fatalf("BuyNotional=%f want=200", w.BuyNotional)
	}
	if math.Abs(w.SellNotional-151.5) > 1e-9 {
		t.Fatalf("SellNotional=%f want=151.5", w.SellNotional)
	}
}

func TestTapeWindowV1_Close_DerivedMetrics(t *testing.T) {
	w, p := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 1000)
	if p != nil {
		t.Fatalf("NewTapeWindowV1: %v", p)
	}
	if p := w.ApplyTrade(100, 1, true, 1); p != nil {
		t.Fatalf("ApplyTrade(1): %v", p)
	}
	if p := w.ApplyTrade(101, 1, false, 2); p != nil {
		t.Fatalf("ApplyTrade(2): %v", p)
	}
	if p := w.Close(2000); p != nil {
		t.Fatalf("Close: %v", p)
	}
	if math.Abs(w.Rate()-2.0) > 1e-9 {
		t.Fatalf("Rate=%f want=2", w.Rate())
	}
	if math.Abs(w.Imbalance()-0.0) > 1e-9 {
		t.Fatalf("Imbalance=%f want=0", w.Imbalance())
	}
	if math.Abs(w.VwapPrice-100.5) > 1e-9 {
		t.Fatalf("VwapPrice=%f want=100.5", w.VwapPrice)
	}
}

func TestTapeWindowV1_BurstDetection(t *testing.T) {
	w, p := domain.NewTapeWindowV1("binance", "BTC-USDT", "250ms", 1000)
	if p != nil {
		t.Fatalf("NewTapeWindowV1: %v", p)
	}
	for i := int64(1); i <= 25; i++ {
		if p := w.ApplyTrade(100, 1, true, i); p != nil {
			t.Fatalf("ApplyTrade(%d): %v", i, p)
		}
	}
	if w.IsBurst(25) {
		t.Fatal("expected threshold boundary to be false")
	}
	if !w.IsBurst(24) {
		t.Fatal("expected above-threshold burst")
	}
}

func TestTapeWindowV1_Imbalance_AllBuys(t *testing.T) {
	w, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
	_ = w.ApplyTrade(100, 1, true, 1)
	_ = w.ApplyTrade(101, 2, true, 2)
	_ = w.Close(1000)
	if math.Abs(w.Imbalance()-1.0) > 1e-9 {
		t.Fatalf("imbalance=%f want=1", w.Imbalance())
	}
}

func TestTapeWindowV1_Imbalance_AllSells(t *testing.T) {
	w, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
	_ = w.ApplyTrade(100, 1, false, 1)
	_ = w.ApplyTrade(101, 2, false, 2)
	_ = w.Close(1000)
	if math.Abs(w.Imbalance()+1.0) > 1e-9 {
		t.Fatalf("imbalance=%f want=-1", w.Imbalance())
	}
}

func TestTapeWindowV1_Imbalance_Empty(t *testing.T) {
	w, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
	if p := w.Close(1000); p != nil {
		t.Fatalf("Close: %v", p)
	}
	if w.Imbalance() != 0 {
		t.Fatalf("imbalance=%f want=0", w.Imbalance())
	}
}

func TestTapeWindowV1_VwapPrice_Empty(t *testing.T) {
	w, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
	if p := w.Close(1000); p != nil {
		t.Fatalf("Close: %v", p)
	}
	if w.VwapPrice != 0 {
		t.Fatalf("VwapPrice=%f want=0", w.VwapPrice)
	}
}

func TestTapeWindowV1_Validate_RejectsBadInputs(t *testing.T) {
	w, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
	if p := w.ApplyTrade(0, 1, true, 1); p == nil {
		t.Fatal("expected price=0 to fail")
	}
	if p := w.ApplyTrade(100, math.NaN(), true, 2); p == nil {
		t.Fatal("expected size=NaN to fail")
	}
	if _, p := domain.NewTapeWindowV1("", "BTC-USDT", "1s", 0); p == nil {
		t.Fatal("expected empty venue to fail")
	}
}

func TestTapeWindowV1_CommutativeProperty(t *testing.T) {
	type trade struct {
		price float64
		size  float64
		buy   bool
		seq   int64
	}
	trades := []trade{
		{100.5, 0.3, true, 2},
		{99.9, 0.7, false, 1},
		{101.2, 1.1, true, 4},
		{100.1, 0.6, false, 3},
	}

	w1, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
	for _, tr := range trades {
		if p := w1.ApplyTrade(tr.price, tr.size, tr.buy, tr.seq); p != nil {
			t.Fatalf("w1 ApplyTrade: %v", p)
		}
	}
	if p := w1.Close(1000); p != nil {
		t.Fatalf("w1 Close: %v", p)
	}

	w2, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
	for i := len(trades) - 1; i >= 0; i-- {
		tr := trades[i]
		if p := w2.ApplyTrade(tr.price, tr.size, tr.buy, tr.seq); p != nil {
			t.Fatalf("w2 ApplyTrade: %v", p)
		}
	}
	if p := w2.Close(1000); p != nil {
		t.Fatalf("w2 Close: %v", p)
	}

	if w1.TradeCount != w2.TradeCount || w1.BuyCount != w2.BuyCount || w1.SellCount != w2.SellCount {
		t.Fatalf("count mismatch w1=%+v w2=%+v", w1, w2)
	}
	assertTapeFloatEq(t, "total_volume", w1.TotalVolume, w2.TotalVolume)
	assertTapeFloatEq(t, "buy_volume", w1.BuyVolume, w2.BuyVolume)
	assertTapeFloatEq(t, "sell_volume", w1.SellVolume, w2.SellVolume)
	assertTapeFloatEq(t, "buy_notional", w1.BuyNotional, w2.BuyNotional)
	assertTapeFloatEq(t, "sell_notional", w1.SellNotional, w2.SellNotional)
	assertTapeFloatEq(t, "vwap", w1.VwapPrice, w2.VwapPrice)
	assertTapeFloatEq(t, "rate", w1.Rate(), w2.Rate())
	assertTapeFloatEq(t, "imbalance", w1.Imbalance(), w2.Imbalance())
	if w1.LastSeq != w2.LastSeq {
		t.Fatalf("LastSeq mismatch %d vs %d", w1.LastSeq, w2.LastSeq)
	}
	assertTapeFloatEq(t, "last_price", w1.LastPrice, w2.LastPrice)
}

func TestTapeWindowV1_MaxTradeSize(t *testing.T) {
	w, _ := domain.NewTapeWindowV1("binance", "BTC-USDT", "1s", 0)
	_ = w.ApplyTrade(100, 1, true, 1)
	_ = w.ApplyTrade(101, 5, true, 2)
	_ = w.ApplyTrade(102, 3, true, 3)
	_ = w.Close(1000)
	if math.Abs(w.MaxTradeSize-5) > 1e-9 {
		t.Fatalf("MaxTradeSize=%f want=5", w.MaxTradeSize)
	}
}

func TestNewTapeWindowV1_InvalidTimeframe(t *testing.T) {
	if _, p := domain.NewTapeWindowV1("binance", "BTC-USDT", "2m", 0); p == nil {
		t.Fatal("expected invalid timeframe failure")
	}
}

func assertTapeFloatEq(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s=%f want=%f", name, got, want)
	}
}
