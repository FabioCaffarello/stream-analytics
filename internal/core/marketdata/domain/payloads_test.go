package domain

import (
	"math"
	"testing"
)

func TestTradeTickV1_ZeroValue(t *testing.T) {
	var tick TradeTickV1
	if tick.Price != 0 {
		t.Fatalf("zero TradeTickV1.Price = %v, want 0", tick.Price)
	}
	if tick.Size != 0 {
		t.Fatalf("zero TradeTickV1.Size = %v, want 0", tick.Size)
	}
	if tick.Side != "" {
		t.Fatalf("zero TradeTickV1.Side = %q, want empty", tick.Side)
	}
	if tick.TradeID != "" {
		t.Fatalf("zero TradeTickV1.TradeID = %q, want empty", tick.TradeID)
	}
	if tick.Timestamp != 0 {
		t.Fatalf("zero TradeTickV1.Timestamp = %d, want 0", tick.Timestamp)
	}
}

func TestTradeTickV1_FieldAccess(t *testing.T) {
	tick := TradeTickV1{
		Price:     50123.45,
		Size:      0.001,
		Side:      "buy",
		TradeID:   "tx-12345",
		Timestamp: 1710000000000,
	}
	if tick.Price != 50123.45 {
		t.Fatalf("Price = %v, want 50123.45", tick.Price)
	}
	if tick.Size != 0.001 {
		t.Fatalf("Size = %v, want 0.001", tick.Size)
	}
	if tick.Side != "buy" {
		t.Fatalf("Side = %q, want %q", tick.Side, "buy")
	}
	if tick.TradeID != "tx-12345" {
		t.Fatalf("TradeID = %q, want %q", tick.TradeID, "tx-12345")
	}
	if tick.Timestamp != 1710000000000 {
		t.Fatalf("Timestamp = %d, want 1710000000000", tick.Timestamp)
	}
}

func TestBookDeltaV1_BidsAsks(t *testing.T) {
	delta := BookDeltaV1{
		Bids: []PriceLevel{
			{Price: 50000.0, Size: 1.5},
			{Price: 49999.0, Size: 0.0}, // size=0 means remove
		},
		Asks: []PriceLevel{
			{Price: 50001.0, Size: 2.0},
		},
		IsSnapshot: false,
		Timestamp:  1710000000000,
	}
	if len(delta.Bids) != 2 {
		t.Fatalf("len(Bids) = %d, want 2", len(delta.Bids))
	}
	if len(delta.Asks) != 1 {
		t.Fatalf("len(Asks) = %d, want 1", len(delta.Asks))
	}
	if delta.IsSnapshot {
		t.Fatal("IsSnapshot should be false for incremental delta")
	}
}

func TestBookDeltaV1_SnapshotFlag(t *testing.T) {
	snap := BookDeltaV1{
		Bids:       []PriceLevel{{Price: 100, Size: 10}},
		Asks:       []PriceLevel{{Price: 101, Size: 5}},
		IsSnapshot: true,
	}
	if !snap.IsSnapshot {
		t.Fatal("IsSnapshot should be true for full snapshot")
	}
}

func TestPriceLevel_ZeroSizeSemantics(t *testing.T) {
	// In order book context, Size=0 means the price level should be removed.
	level := PriceLevel{Price: 50000.0, Size: 0}
	if level.Size != 0 {
		t.Fatalf("Size = %v, want 0 (remove semantics)", level.Size)
	}
	if level.Price != 50000.0 {
		t.Fatalf("Price = %v, want 50000.0", level.Price)
	}
}

func TestPriceLevel_BoundaryValues(t *testing.T) {
	tests := []struct {
		name  string
		price float64
		size  float64
	}{
		{name: "zero price and size", price: 0, size: 0},
		{name: "very small", price: math.SmallestNonzeroFloat64, size: math.SmallestNonzeroFloat64},
		{name: "very large", price: math.MaxFloat64, size: math.MaxFloat64},
		{name: "typical", price: 50000.50, size: 1.234},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pl := PriceLevel{Price: tc.price, Size: tc.size}
			if pl.Price != tc.price {
				t.Fatalf("Price = %v, want %v", pl.Price, tc.price)
			}
			if pl.Size != tc.size {
				t.Fatalf("Size = %v, want %v", pl.Size, tc.size)
			}
		})
	}
}

func TestBookDeltaV1_EmptySlices(t *testing.T) {
	delta := BookDeltaV1{
		Bids:      nil,
		Asks:      nil,
		Timestamp: 1710000000000,
	}
	if delta.Bids != nil {
		t.Fatal("nil Bids should remain nil")
	}
	if delta.Asks != nil {
		t.Fatal("nil Asks should remain nil")
	}
}

func TestBookDeltaV1_SequenceFields(t *testing.T) {
	delta := BookDeltaV1{
		FirstID:   100,
		FinalID:   105,
		PrevFinal: 99,
	}
	if delta.FirstID != 100 {
		t.Fatalf("FirstID = %d, want 100", delta.FirstID)
	}
	if delta.FinalID != 105 {
		t.Fatalf("FinalID = %d, want 105", delta.FinalID)
	}
	if delta.PrevFinal != 99 {
		t.Fatalf("PrevFinal = %d, want 99", delta.PrevFinal)
	}
}

func TestMarkPriceTickV1_FundingRate(t *testing.T) {
	tests := []struct {
		name        string
		markPrice   float64
		indexPrice  float64
		fundingRate float64
	}{
		{name: "zero funding rate", markPrice: 50000, indexPrice: 50000, fundingRate: 0},
		{name: "positive funding rate", markPrice: 50001, indexPrice: 50000, fundingRate: 0.0001},
		{name: "negative funding rate", markPrice: 49999, indexPrice: 50000, fundingRate: -0.0003},
		{name: "very large funding rate", markPrice: 50000, indexPrice: 50000, fundingRate: 0.05},
		{name: "smallest nonzero", markPrice: math.SmallestNonzeroFloat64, indexPrice: 0, fundingRate: math.SmallestNonzeroFloat64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tick := MarkPriceTickV1{
				MarkPrice:   tc.markPrice,
				IndexPrice:  tc.indexPrice,
				FundingRate: tc.fundingRate,
				Timestamp:   1710000000000,
			}
			if tick.MarkPrice != tc.markPrice {
				t.Fatalf("MarkPrice = %v, want %v", tick.MarkPrice, tc.markPrice)
			}
			if tick.IndexPrice != tc.indexPrice {
				t.Fatalf("IndexPrice = %v, want %v", tick.IndexPrice, tc.indexPrice)
			}
			if tick.FundingRate != tc.fundingRate {
				t.Fatalf("FundingRate = %v, want %v", tick.FundingRate, tc.fundingRate)
			}
		})
	}
}

func TestMarkPriceTickV1_ZeroValue(t *testing.T) {
	var tick MarkPriceTickV1
	if tick.MarkPrice != 0 {
		t.Fatalf("zero MarkPriceTickV1.MarkPrice = %v, want 0", tick.MarkPrice)
	}
	if tick.FundingRate != 0 {
		t.Fatalf("zero MarkPriceTickV1.FundingRate = %v, want 0", tick.FundingRate)
	}
	if tick.Timestamp != 0 {
		t.Fatalf("zero MarkPriceTickV1.Timestamp = %d, want 0", tick.Timestamp)
	}
}

func TestLiquidationTickV1_Sides(t *testing.T) {
	tests := []struct {
		name  string
		side  string
		price float64
		size  float64
	}{
		{name: "buy side", side: "buy", price: 50000, size: 1.0},
		{name: "sell side", side: "sell", price: 49000, size: 0.5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			liq := LiquidationTickV1{
				Side:      tc.side,
				Price:     tc.price,
				Size:      tc.size,
				Timestamp: 1710000000000,
			}
			if liq.Side != tc.side {
				t.Fatalf("Side = %q, want %q", liq.Side, tc.side)
			}
			if liq.Price != tc.price {
				t.Fatalf("Price = %v, want %v", liq.Price, tc.price)
			}
			if liq.Size != tc.size {
				t.Fatalf("Size = %v, want %v", liq.Size, tc.size)
			}
		})
	}
}

func TestLiquidationTickV1_BoundaryValues(t *testing.T) {
	tests := []struct {
		name  string
		price float64
		size  float64
	}{
		{name: "zero values", price: 0, size: 0},
		{name: "max float64", price: math.MaxFloat64, size: math.MaxFloat64},
		{name: "smallest nonzero float64", price: math.SmallestNonzeroFloat64, size: math.SmallestNonzeroFloat64},
		{name: "typical values", price: 48500.25, size: 12.345},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			liq := LiquidationTickV1{
				Side:      "sell",
				Price:     tc.price,
				Size:      tc.size,
				Timestamp: 1710000000000,
			}
			if liq.Price != tc.price {
				t.Fatalf("Price = %v, want %v", liq.Price, tc.price)
			}
			if liq.Size != tc.size {
				t.Fatalf("Size = %v, want %v", liq.Size, tc.size)
			}
		})
	}
}
