package storage

import (
	"context"
	"testing"

	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
)

func BenchmarkMarshalCandle(b *testing.B) {
	c := aggdomain.CandleV1{
		Venue:         "binance",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1,
		WindowEndTs:   60,
		IsClosed:      true,
		Open:          100.0,
		High:          101.0,
		Low:           99.0,
		ClosePrice:    100.5,
		Volume:        123.45,
		BuyVolume:     100.0,
		SellVolume:    23.45,
		TradeCount:    10,
		SeqFirst:      1,
		SeqLast:       10,
	}
	b.ReportAllocs()
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		if _, _, p := MarshalCandle(ctx, c); p != nil {
			b.Fatalf("marshal failed: %v", p)
		}
	}
}
