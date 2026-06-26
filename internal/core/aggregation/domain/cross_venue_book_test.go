package domain

import (
	"math"
	"testing"
)

func TestCrossVenueBookMerge_TwoVenuesDeterministic(t *testing.T) {
	merger := DeterministicCrossVenueBookMerger{}
	nowMs := int64(100_000)

	snapshot, prob := merger.Merge("BTCUSDT", nowMs, []CrossVenueVenueBook{
		{
			Venue:    "BINANCE",
			TsIngest: 99_990,
			BestBid:  levelPtr(100.0, 1.0),
			BestAsk:  levelPtr(101.0, 2.0),
		},
		{
			Venue:    "BYBIT",
			TsIngest: 99_991,
			BestBid:  levelPtr(100.5, 1.5),
			BestAsk:  levelPtr(101.2, 2.5),
		},
	}, 30_000)
	if prob != nil {
		t.Fatalf("merge failed: %v", prob)
	}

	if got := len(snapshot.BestBids); got != 2 {
		t.Fatalf("best_bids len=%d want=2", got)
	}
	if got := len(snapshot.BestAsks); got != 2 {
		t.Fatalf("best_asks len=%d want=2", got)
	}
	if snapshot.BestBids[0].Venue != "BYBIT" || snapshot.BestBids[1].Venue != "BINANCE" {
		t.Fatalf("best_bids order=%s,%s want=BYBIT,BINANCE", snapshot.BestBids[0].Venue, snapshot.BestBids[1].Venue)
	}
	if snapshot.BestAsks[0].Venue != "BINANCE" || snapshot.BestAsks[1].Venue != "BYBIT" {
		t.Fatalf("best_asks order=%s,%s want=BINANCE,BYBIT", snapshot.BestAsks[0].Venue, snapshot.BestAsks[1].Venue)
	}
	if snapshot.GlobalSpreadBPS <= 0 {
		t.Fatalf("global_spread_bps=%f want>0", snapshot.GlobalSpreadBPS)
	}
}

func TestCrossVenueBookMerge_ThreeVenuesTieBreakByVenue(t *testing.T) {
	merger := DeterministicCrossVenueBookMerger{}
	nowMs := int64(100_000)

	snapshot, prob := merger.Merge("ETHUSDT", nowMs, []CrossVenueVenueBook{
		{
			Venue:    "BYBIT",
			TsIngest: 99_990,
			BestBid:  levelPtr(100.0, 1.0),
			BestAsk:  levelPtr(100.9, 2.0),
		},
		{
			Venue:    "BINANCE",
			TsIngest: 99_991,
			BestBid:  levelPtr(100.0, 1.5),
			BestAsk:  levelPtr(100.8, 2.2),
		},
		{
			Venue:    "COINBASE",
			TsIngest: 99_992,
			BestBid:  levelPtr(99.9, 1.8),
			BestAsk:  levelPtr(100.7, 2.4),
		},
	}, 30_000)
	if prob != nil {
		t.Fatalf("merge failed: %v", prob)
	}

	if got := len(snapshot.BestBids); got != 3 {
		t.Fatalf("best_bids len=%d want=3", got)
	}
	if got := len(snapshot.BestAsks); got != 3 {
		t.Fatalf("best_asks len=%d want=3", got)
	}
	if snapshot.BestBids[0].Venue != "BINANCE" || snapshot.BestBids[1].Venue != "BYBIT" || snapshot.BestBids[2].Venue != "COINBASE" {
		t.Fatalf(
			"best_bids order=%s,%s,%s want=BINANCE,BYBIT,COINBASE",
			snapshot.BestBids[0].Venue,
			snapshot.BestBids[1].Venue,
			snapshot.BestBids[2].Venue,
		)
	}
	if snapshot.BestAsks[0].Venue != "COINBASE" || snapshot.BestAsks[1].Venue != "BINANCE" || snapshot.BestAsks[2].Venue != "BYBIT" {
		t.Fatalf(
			"best_asks order=%s,%s,%s want=COINBASE,BINANCE,BYBIT",
			snapshot.BestAsks[0].Venue,
			snapshot.BestAsks[1].Venue,
			snapshot.BestAsks[2].Venue,
		)
	}
}

func TestCrossVenueBookMerge_StaleVenueExcluded(t *testing.T) {
	merger := DeterministicCrossVenueBookMerger{}
	nowMs := int64(60_000)

	snapshot, prob := merger.Merge("BTCUSDT", nowMs, []CrossVenueVenueBook{
		{
			Venue:    "BINANCE",
			TsIngest: 20_000, // stale when threshold is 30s
			BestBid:  levelPtr(100.0, 1.0),
			BestAsk:  levelPtr(101.0, 1.0),
		},
		{
			Venue:    "BYBIT",
			TsIngest: 59_000,
			BestBid:  levelPtr(100.2, 1.0),
			BestAsk:  levelPtr(100.9, 1.0),
		},
	}, 30_000)
	if prob != nil {
		t.Fatalf("merge failed: %v", prob)
	}
	if got := len(snapshot.BestBids); got != 1 {
		t.Fatalf("best_bids len=%d want=1", got)
	}
	if got := snapshot.BestBids[0].Venue; got != "BYBIT" {
		t.Fatalf("best_bids[0].venue=%s want=BYBIT", got)
	}
}

func TestCrossVenueBookMerge_EmptyVenueInputsProduceEmptySnapshot(t *testing.T) {
	merger := DeterministicCrossVenueBookMerger{}
	nowMs := int64(10_000)

	snapshot, prob := merger.Merge("BTCUSDT", nowMs, []CrossVenueVenueBook{
		{
			Venue:    "BINANCE",
			TsIngest: nowMs,
			BestBid:  nil,
			BestAsk:  levelPtr(101.0, 1.0),
		},
		{
			Venue:    "BYBIT",
			TsIngest: nowMs,
			BestBid:  levelPtr(100.0, 1.0),
			BestAsk:  nil,
		},
	}, 30_000)
	if prob != nil {
		t.Fatalf("merge failed: %v", prob)
	}
	if got := len(snapshot.BestBids); got != 0 {
		t.Fatalf("best_bids len=%d want=0", got)
	}
	if got := len(snapshot.BestAsks); got != 0 {
		t.Fatalf("best_asks len=%d want=0", got)
	}
}

func TestCrossVenueBookMerge_SpreadAndDivergence(t *testing.T) {
	merger := DeterministicCrossVenueBookMerger{}
	nowMs := int64(90_000)

	snapshot, prob := merger.Merge("BTCUSDT", nowMs, []CrossVenueVenueBook{
		{
			Venue:    "BINANCE",
			TsIngest: 89_000,
			BestBid:  levelPtr(100.0, 1.0),
			BestAsk:  levelPtr(101.0, 1.0),
		},
		{
			Venue:    "BYBIT",
			TsIngest: 89_100,
			BestBid:  levelPtr(100.5, 1.0),
			BestAsk:  levelPtr(101.5, 1.0),
		},
	}, 30_000)
	if prob != nil {
		t.Fatalf("merge failed: %v", prob)
	}

	wantSpread := ((101.0 - 100.5) / ((101.0 + 100.5) / 2.0)) * 10_000.0
	if !almostEqual(snapshot.GlobalSpreadBPS, wantSpread) {
		t.Fatalf("global_spread_bps=%f want=%f", snapshot.GlobalSpreadBPS, wantSpread)
	}
	if snapshot.VenueDivergenceBPS <= 0 {
		t.Fatalf("venue_divergence_bps=%f want>0", snapshot.VenueDivergenceBPS)
	}
}

func levelPtr(price, quantity float64) *Level {
	return &Level{
		Price:    Price(price),
		Quantity: Quantity(quantity),
	}
}

func almostEqual(got, want float64) bool {
	const epsilon = 1e-9
	return math.Abs(got-want) <= epsilon
}
