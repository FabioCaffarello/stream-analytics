package domain_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
)

func BenchmarkApplyDelta_1000Levels(b *testing.B) {
	b.ReportAllocs()

	book, p := domain.NewOrderBook("binance", "BTCUSDT")
	if p != nil {
		b.Fatalf("NewOrderBook failed: %v", p)
	}

	const levelsPerSide = 1000
	bids := make([]domain.Level, levelsPerSide)
	asks := make([]domain.Level, levelsPerSide)
	for i := 0; i < levelsPerSide; i++ {
		bids[i] = domain.Level{
			Price:    domain.Price(100_000 - i),
			Quantity: domain.Quantity(1 + (i % 10)),
		}
		asks[i] = domain.Level{
			Price:    domain.Price(101_000 + i),
			Quantity: domain.Quantity(1 + (i % 10)),
		}
	}

	var seq int64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seq++
		if prob := book.ApplyDelta(seq, bids, asks); prob != nil {
			b.Fatalf("ApplyDelta failed at iteration=%d: %v", i, prob)
		}
	}
}
