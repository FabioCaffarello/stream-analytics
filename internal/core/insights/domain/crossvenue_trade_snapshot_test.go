package domain

import "testing"

func TestCrossVenueTradeSnapshotV1_Validate(t *testing.T) {
	s := CrossVenueTradeSnapshotV1{
		Instrument:        "BTCUSDT",
		MarketType:        "SPOT",
		WatermarkTsIngest: 1_710_000_000_123,
		Venues: []SnapshotVenueTradeV1{
			{Venue: "BINANCE", Price: 100.25, Size: 1.5, Side: "buy", TsExchange: 1_710_000_000_100, TsIngest: 1_710_000_000_120, Seq: 11},
			{Venue: "BYBIT", Price: 100.35, Size: 2.0, Side: "sell", TsExchange: 1_710_000_000_110, TsIngest: 1_710_000_000_123, Seq: 7},
		},
		MinPrice:      100.25,
		MinPriceVenue: "BINANCE",
		MaxPrice:      100.35,
		MaxPriceVenue: "BYBIT",
		SpreadAbs:     0.10,
		SpreadBps:     9.9701,
		MidPrice:      100.30,
	}

	if p := s.Validate(); p != nil {
		t.Fatalf("Validate() unexpected problem: %v", p)
	}
}

func TestCrossVenueTradeSnapshotV1_ValidateFailsOnMissingDerivedVenue(t *testing.T) {
	s := CrossVenueTradeSnapshotV1{
		Instrument:        "BTCUSDT",
		WatermarkTsIngest: 1_710_000_000_123,
		Venues: []SnapshotVenueTradeV1{
			{Venue: "BINANCE", Price: 100.25, Size: 1.5, Side: "buy", TsExchange: 1_710_000_000_100, TsIngest: 1_710_000_000_120, Seq: 11},
			{Venue: "BYBIT", Price: 100.35, Size: 2.0, Side: "sell", TsExchange: 1_710_000_000_110, TsIngest: 1_710_000_000_123, Seq: 7},
		},
		MinPrice:      100.25,
		MaxPrice:      100.35,
		MaxPriceVenue: "BYBIT",
		SpreadAbs:     0.10,
		SpreadBps:     9.9701,
		MidPrice:      100.30,
	}

	if p := s.Validate(); p == nil {
		t.Fatal("expected validation problem for missing min_price_venue")
	}
}

func TestCrossVenueSpreadSignalV1_Validate(t *testing.T) {
	s := CrossVenueSpreadSignalV1{
		Instrument:        "BTCUSDT",
		MarketType:        "SPOT",
		WatermarkTsIngest: 1_710_000_000_123,
		MinPrice:          100.25,
		MinPriceVenue:     "BINANCE",
		MaxPrice:          100.35,
		MaxPriceVenue:     "BYBIT",
		SpreadAbs:         0.10,
		SpreadBps:         9.9701,
	}
	if p := s.Validate(); p != nil {
		t.Fatalf("Validate() unexpected problem: %v", p)
	}
}
