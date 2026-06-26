package domain_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
)

func BenchmarkCandleRollup_5x1mTo5m(b *testing.B) {
	candles := makeClosedCandlesForBench(b, 5)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, p := domain.RollupCandle(candles, "5m"); p != nil {
			b.Fatal(p)
		}
	}
}

func BenchmarkCandleRollup_60x1mTo1h(b *testing.B) {
	candles := makeClosedCandlesForBench(b, 60)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, p := domain.RollupCandle(candles, "1h"); p != nil {
			b.Fatal(p)
		}
	}
}

func BenchmarkCandleRollup_240x1mTo4h(b *testing.B) {
	candles := makeClosedCandlesForBench(b, 240)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, p := domain.RollupCandle(candles, "4h"); p != nil {
			b.Fatal(p)
		}
	}
}

func BenchmarkCandleRollup_1440x1mTo1d(b *testing.B) {
	candles := makeClosedCandlesForBench(b, 1440)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, p := domain.RollupCandle(candles, "1d"); p != nil {
			b.Fatal(p)
		}
	}
}

func makeClosedCandlesForBench(b *testing.B, n int) []domain.CandleV1 {
	b.Helper()
	candles := make([]domain.CandleV1, n)
	for i := range candles {
		c, p := domain.NewCandleV1("BINANCE", "BTCUSDT", "1m", int64(i)*60_000)
		if p != nil {
			b.Fatal(p)
		}
		if p := c.ApplyTrade(50000+float64(i), 1.0, true, int64(i*2+1)); p != nil {
			b.Fatal(p)
		}
		if p := c.ApplyTrade(50000+float64(i)+50, 0.5, false, int64(i*2+2)); p != nil {
			b.Fatal(p)
		}
		if p := c.Close(int64(i)*60_000 + 60_000); p != nil {
			b.Fatal(p)
		}
		candles[i] = *c
	}
	return candles
}
