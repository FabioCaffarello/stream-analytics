package timescale_test

import (
	"context"
	"strings"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/timescale"
	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
)

func TestPgStatsWriter_Save_Success(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgStatsWriterWithExecutor(exec)

	if p := w.SaveStats(context.Background(), testStatsClosed(true, true)); p != nil {
		t.Fatalf("save stats: %v", p)
	}
	if !strings.Contains(exec.lastQuery, "aggregation_stats") {
		t.Fatalf("query=%q missing target table", exec.lastQuery)
	}
	if len(exec.lastArgs) != 18 {
		t.Fatalf("args len=%d want=18", len(exec.lastArgs))
	}
	if exec.lastArgs[9] == nil || exec.lastArgs[13] == nil {
		t.Fatal("expected nullable mark/funding fields to be populated")
	}
}

func TestPgStatsWriter_Save_PartialInputs(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgStatsWriterWithExecutor(exec)

	if p := w.SaveStats(context.Background(), testStatsClosed(false, false)); p != nil {
		t.Fatalf("save stats: %v", p)
	}
	if exec.lastArgs[9] != nil || exec.lastArgs[10] != nil || exec.lastArgs[11] != nil || exec.lastArgs[12] != nil {
		t.Fatalf("expected nil markprice fields, got %#v", exec.lastArgs[9:13])
	}
	if exec.lastArgs[13] != nil || exec.lastArgs[14] != nil {
		t.Fatalf("expected nil funding fields, got %#v", exec.lastArgs[13:15])
	}
}

func TestPgStatsWriter_Save_NullableFields(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgStatsWriterWithExecutor(exec)

	if p := w.SaveStats(context.Background(), testStatsClosed(true, false)); p != nil {
		t.Fatalf("save stats: %v", p)
	}
	if exec.lastArgs[9] == nil {
		t.Fatal("expected markprice_open to be non-nil")
	}
	if exec.lastArgs[13] != nil || exec.lastArgs[14] != nil {
		t.Fatalf("expected funding fields nil when absent, got %#v", exec.lastArgs[13:15])
	}
}

func testStatsClosed(withMarkPrice, withFunding bool) aggdomain.StatsWindowClosed {
	stats := aggdomain.StatsWindowV1{
		Venue:          "binance",
		Instrument:     "BTCUSDT",
		Timeframe:      "1m",
		WindowStartTs:  1_710_000_000_000,
		WindowEndTs:    1_710_000_060_000,
		WindowMs:       60_000,
		LiqBuyVolume:   5,
		LiqSellVolume:  3,
		LiqTotalVolume: 8,
		LiqCount:       2,
		SeqFirst:       100,
		SeqLast:        110,
		IsClosed:       true,
	}
	if withMarkPrice {
		stats.MarkPriceOpen = 100.0
		stats.MarkPriceHigh = 102.0
		stats.MarkPriceLow = 99.0
		stats.MarkPriceClose = 101.0
	}
	if withFunding {
		stats.FundingRateAvg = 0.0001
		stats.FundingRateLast = 0.0002
	}
	return aggdomain.StatsWindowClosed{Stats: stats}
}
