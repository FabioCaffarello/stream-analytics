package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/domain"
)

func TestMarkPriceDedupStrongKey(t *testing.T) {
	uc := app.NewNormalizeMarkPriceLiquidation(app.NormalizeMarkPriceLiquidationConfig{DedupWindowSize: 16})
	req := app.NormalizeMarkPriceLiquidationRequest{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		EventType:  "marketdata.markprice",
		Version:    1,
		TsExchange: 1_710_000_000_001,
		TsIngest:   1_710_000_000_100,
		MarkPricePayload: &domain.MarkPriceTickV1{
			MarkPrice:   50000.0,
			IndexPrice:  49990.0,
			FundingRate: 0.0001,
			Timestamp:   1_710_000_000_001,
		},
	}

	r1 := uc.Execute(context.Background(), req)
	if r1.IsFail() {
		t.Fatalf("first execute failed: %v", r1.Problem())
	}
	out1 := r1.Value()
	if out1.DedupKey == "" {
		t.Fatal("dedup key must not be empty")
	}
	if out1.IsDuplicate {
		t.Fatal("first event must not be duplicate")
	}

	r2 := uc.Execute(context.Background(), req)
	if r2.IsFail() {
		t.Fatalf("second execute failed: %v", r2.Problem())
	}
	out2 := r2.Value()
	if !out2.IsDuplicate {
		t.Fatal("same markprice event should be duplicate")
	}
	if out2.DedupKey != out1.DedupKey {
		t.Fatalf("dedup keys mismatch: %q != %q", out2.DedupKey, out1.DedupKey)
	}

	req.MarkPricePayload.MarkPrice = 50001.0
	r3 := uc.Execute(context.Background(), req)
	if r3.IsFail() {
		t.Fatalf("third execute failed: %v", r3.Problem())
	}
	out3 := r3.Value()
	if out3.IsDuplicate {
		t.Fatal("different markprice payload must not be duplicate")
	}
	if out3.DedupKey == out1.DedupKey {
		t.Fatal("dedup key should change with payload change")
	}
}

func TestLiquidationDedupStrongKey(t *testing.T) {
	uc := app.NewNormalizeMarkPriceLiquidation(app.NormalizeMarkPriceLiquidationConfig{DedupWindowSize: 16})
	req := app.NormalizeMarkPriceLiquidationRequest{
		Venue:      "bybit",
		Instrument: "eth/usdt",
		EventType:  "marketdata.liquidation",
		Version:    1,
		TsExchange: 1_710_000_100_001,
		TsIngest:   1_710_000_100_100,
		LiquidationPayload: &domain.LiquidationTickV1{
			Side:      "SELL",
			Price:     3200.5,
			Size:      12.5,
			Timestamp: 1_710_000_100_001,
		},
	}

	r1 := uc.Execute(context.Background(), req)
	if r1.IsFail() {
		t.Fatalf("first execute failed: %v", r1.Problem())
	}
	out1 := r1.Value()
	if out1.DedupKey == "" {
		t.Fatal("dedup key must not be empty")
	}
	if out1.IsDuplicate {
		t.Fatal("first liquidation event must not be duplicate")
	}

	r2 := uc.Execute(context.Background(), req)
	if r2.IsFail() {
		t.Fatalf("second execute failed: %v", r2.Problem())
	}
	if !r2.Value().IsDuplicate {
		t.Fatal("same liquidation event should be duplicate")
	}

	req.LiquidationPayload.Side = "BUY"
	req.TsIngest += int64(time.Millisecond)
	r3 := uc.Execute(context.Background(), req)
	if r3.IsFail() {
		t.Fatalf("third execute failed: %v", r3.Problem())
	}
	out3 := r3.Value()
	if out3.IsDuplicate {
		t.Fatal("liquidation with different side must not be duplicate")
	}
	if out3.DedupKey == out1.DedupKey {
		t.Fatal("dedup key should change with liquidation side change")
	}
}

func TestMarkPriceLiquidationCanonicalNormalization(t *testing.T) {
	uc := app.NewNormalizeMarkPriceLiquidation(app.NormalizeMarkPriceLiquidationConfig{DedupWindowSize: 8})

	markReq := app.NormalizeMarkPriceLiquidationRequest{
		Venue:      "  Binance ",
		Instrument: " btc/usdt ",
		EventType:  "MARKETDATA.MARKPRICE",
		Version:    1,
		TsExchange: 1_710_000_200_001,
		TsIngest:   1_710_000_200_100,
		MarkPricePayload: &domain.MarkPriceTickV1{
			MarkPrice:   50100,
			IndexPrice:  50090,
			FundingRate: 0.0002,
			Timestamp:   1_710_000_200_001,
		},
	}
	markRes := uc.Execute(context.Background(), markReq)
	if markRes.IsFail() {
		t.Fatalf("markprice normalize failed: %v", markRes.Problem())
	}
	markOut := markRes.Value()
	if markOut.Venue != "BINANCE" {
		t.Fatalf("venue=%q want BINANCE", markOut.Venue)
	}
	if markOut.Instrument != "BTCUSDT" {
		t.Fatalf("instrument=%q want BTCUSDT", markOut.Instrument)
	}
	if markOut.EventType != "marketdata.markprice" {
		t.Fatalf("event_type=%q want marketdata.markprice", markOut.EventType)
	}

	liqReq := app.NormalizeMarkPriceLiquidationRequest{
		Venue:      " ByBit ",
		Instrument: " eth-usdt ",
		EventType:  "MARKETDATA.LIQUIDATION",
		Version:    1,
		TsExchange: 1_710_000_300_001,
		TsIngest:   1_710_000_300_100,
		LiquidationPayload: &domain.LiquidationTickV1{
			Side:      " SELL ",
			Price:     3201,
			Size:      4,
			Timestamp: 1_710_000_300_001,
		},
	}
	liqRes := uc.Execute(context.Background(), liqReq)
	if liqRes.IsFail() {
		t.Fatalf("liquidation normalize failed: %v", liqRes.Problem())
	}
	liqOut := liqRes.Value()
	if liqOut.Venue != "BYBIT" {
		t.Fatalf("venue=%q want BYBIT", liqOut.Venue)
	}
	if liqOut.Instrument != "ETHUSDT" {
		t.Fatalf("instrument=%q want ETHUSDT", liqOut.Instrument)
	}
	if liqOut.EventType != "marketdata.liquidation" {
		t.Fatalf("event_type=%q want marketdata.liquidation", liqOut.EventType)
	}
	if liqOut.Liquidation == nil || liqOut.Liquidation.Side != "sell" {
		t.Fatalf("normalized side mismatch: %+v", liqOut.Liquidation)
	}
}
