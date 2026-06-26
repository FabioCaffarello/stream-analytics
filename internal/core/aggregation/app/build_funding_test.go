package app_test

import (
	"context"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/app"
	mddomain "github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
)

func TestBuildFundingRate_FromMarkPrice_InvokesStats(t *testing.T) {
	uc, _, _ := newStatsUC(1_000)
	fundingUC := app.NewBuildFundingRateFromEvents(uc)

	_, p := fundingUC.Execute(context.Background(), "binance", "BTCUSDT", 1, 1, mddomain.MarkPriceTickV1{
		MarkPrice:   50000.0,
		IndexPrice:  49990.0,
		FundingRate: 0.0002,
		Timestamp:   1,
	})
	if p != nil {
		t.Fatalf("Execute failed: %v", p)
	}
	if uc.ActiveWindows() == 0 {
		t.Fatalf("expected at least one active window after funding input")
	}
}
