package app_test

import (
	"context"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func TestAggregationService_SnapshotOrderBook(t *testing.T) {
	pub := &fakePublisher{}
	store := &fakeStore{}
	svc := app.NewAggregationService(app.AggregationServiceConfig{
		Update:    app.UpdateConfig{},
		Candle:    app.BuildCandleConfig{},
		Stats:     app.BuildStatsConfig{},
		Publisher: pub,
		Store:     store,
	})

	res := svc.UpdateBook.Execute(context.Background(), app.UpdateRequest{
		Venue:      "BINANCE",
		Instrument: "BTC-USDT",
		Seq:        42,
		Bids: []domain.Level{
			{Price: 101, Quantity: 3},
			{Price: 100, Quantity: 1},
		},
		Asks: []domain.Level{
			{Price: 102, Quantity: 2},
		},
	})
	if res.IsFail() {
		t.Fatalf("UpdateBook.Execute failed: %v", res.Problem())
	}

	snapRes := svc.SnapshotOrderBook(context.Background(), domain.BookID{
		Venue:      "BINANCE",
		Instrument: "BTC-USDT",
	})
	if snapRes.IsFail() {
		t.Fatalf("SnapshotOrderBook failed: %v", snapRes.Problem())
	}
	snap := snapRes.Value()
	if got, want := snap.Seq, int64(42); got != want {
		t.Fatalf("snapshot seq=%d want=%d", got, want)
	}
	if got, want := len(snap.Bids), 2; got != want {
		t.Fatalf("snapshot bids=%d want=%d", got, want)
	}
	if got, want := len(snap.Asks), 1; got != want {
		t.Fatalf("snapshot asks=%d want=%d", got, want)
	}
}

func TestAggregationService_SnapshotOrderBook_NotFound(t *testing.T) {
	pub := &fakePublisher{}
	store := &fakeStore{}
	svc := app.NewAggregationService(app.AggregationServiceConfig{
		Update:    app.UpdateConfig{},
		Candle:    app.BuildCandleConfig{},
		Stats:     app.BuildStatsConfig{},
		Publisher: pub,
		Store:     store,
	})

	snapRes := svc.SnapshotOrderBook(context.Background(), domain.BookID{
		Venue:      "BINANCE",
		Instrument: "BTC-USDT",
	})
	if snapRes.IsOk() {
		t.Fatal("expected not-found snapshot")
	}
	if got, want := snapRes.Problem().Code, problem.NotFound; got != want {
		t.Fatalf("problem code=%s want=%s", got, want)
	}
}
