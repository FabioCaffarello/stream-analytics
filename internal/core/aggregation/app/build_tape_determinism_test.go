package app_test

import (
	"context"
	"testing"

	"github.com/market-raccoon/internal/core/aggregation/app"
)

func TestBuildTapeFromTrades_MultiReplica(t *testing.T) {
	replica1, _, _ := newTapeUC(2_000)
	replica2, _, _ := newTapeUC(2_000)

	for seq := int64(1); seq <= 128; seq++ {
		req := app.BuildTapeRequest{
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Price:      100 + float64(seq%7),
			Quantity:   0.1 + float64(seq%5)*0.1,
			IsBuy:      seq%2 == 0,
			Seq:        seq,
			TsIngest:   1_000,
		}
		if _, p := replica1.Execute(context.Background(), req); p != nil {
			t.Fatalf("replica1 Execute seq=%d: %v", seq, p)
		}
	}
	for seq := int64(128); seq >= 1; seq-- {
		req := app.BuildTapeRequest{
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Price:      100 + float64(seq%7),
			Quantity:   0.1 + float64(seq%5)*0.1,
			IsBuy:      seq%2 == 0,
			Seq:        seq,
			TsIngest:   1_000,
		}
		if _, p := replica2.Execute(context.Background(), req); p != nil {
			t.Fatalf("replica2 Execute seq=%d: %v", seq, p)
		}
	}

	left, p := replica1.Execute(context.Background(), app.BuildTapeRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 101, Quantity: 1, IsBuy: true, Seq: 129, TsIngest: 6_000,
	})
	if p != nil {
		t.Fatalf("replica1 flush: %v", p)
	}
	right, p := replica2.Execute(context.Background(), app.BuildTapeRequest{
		Venue: "binance", Instrument: "BTCUSDT", Price: 101, Quantity: 1, IsBuy: true, Seq: 129, TsIngest: 6_000,
	})
	if p != nil {
		t.Fatalf("replica2 flush: %v", p)
	}

	assertTapeCloseEventsEqual(t, left.ClosedWindows, right.ClosedWindows)
}
