package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func testStatsWindowClosed() aggdomain.StatsWindowClosed {
	return aggdomain.StatsWindowClosed{
		Stats: aggdomain.StatsWindowV1{
			Venue:           "binance",
			Instrument:      "BTCUSDT",
			Timeframe:       "1m",
			WindowStartTs:   1_710_000_000_000,
			WindowEndTs:     1_710_000_060_000,
			LiqBuyVolume:    2.5,
			LiqSellVolume:   1.0,
			LiqTotalVolume:  3.5,
			LiqCount:        2,
			MarkPriceOpen:   100.0,
			MarkPriceHigh:   101.0,
			MarkPriceLow:    99.5,
			MarkPriceClose:  100.5,
			FundingRateAvg:  0.0002,
			FundingRateLast: 0.0001,
			SeqFirst:        1,
			SeqLast:         10,
			IsClosed:        true,
		},
	}
}

func TestChStatsWriter_Save_Success_AllFields(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChStatsWriterWithPreparer(&fakePreparer{batch: batch})

	if p := w.SaveStats(context.Background(), testStatsWindowClosed()); p != nil {
		t.Fatalf("save stats: %v", p)
	}
	if len(batch.rows) != 1 {
		t.Fatalf("rows=%d want=1", len(batch.rows))
	}
	// 18 columns: venue, instrument, timeframe, window_start, window_end,
	// liq_buy, liq_sell, liq_total, liq_count, mark_open, mark_high,
	// mark_low, mark_close, funding_avg, funding_last, seq_first,
	// seq_last, idempotency_key
	if len(batch.rows[0]) != 18 {
		t.Fatalf("columns=%d want=18", len(batch.rows[0]))
	}
	if batch.flushes != 1 {
		t.Fatalf("flushes=%d want=1", batch.flushes)
	}
	// Mark price fields should be non-nil float64.
	markOpen := batch.rows[0][9]
	if markOpen == nil {
		t.Fatal("mark_open should not be nil for populated stats")
	}
	// Funding rate fields should be non-nil.
	fundingAvg := batch.rows[0][13]
	if fundingAvg == nil {
		t.Fatal("funding_avg should not be nil for populated stats")
	}
}

func TestChStatsWriter_Save_NullableMarkPrice(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChStatsWriterWithPreparer(&fakePreparer{batch: batch})

	evt := testStatsWindowClosed()
	evt.Stats.MarkPriceOpen = 0
	evt.Stats.MarkPriceHigh = 0
	evt.Stats.MarkPriceLow = 0
	evt.Stats.MarkPriceClose = 0

	if p := w.SaveStats(context.Background(), evt); p != nil {
		t.Fatalf("save stats: %v", p)
	}
	// When mark price is zero, nullable columns should be nil.
	markOpen := batch.rows[0][9]
	if markOpen != nil {
		t.Fatalf("mark_open=%v want=nil for zero mark price", markOpen)
	}
}

func TestChStatsWriter_Save_NullableFundingRate(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChStatsWriterWithPreparer(&fakePreparer{batch: batch})

	evt := testStatsWindowClosed()
	evt.Stats.FundingRateAvg = 0
	evt.Stats.FundingRateLast = 0

	if p := w.SaveStats(context.Background(), evt); p != nil {
		t.Fatalf("save stats: %v", p)
	}
	// When funding rate is zero, nullable columns should be nil.
	fundingAvg := batch.rows[0][13]
	if fundingAvg != nil {
		t.Fatalf("funding_avg=%v want=nil for zero funding rate", fundingAvg)
	}
}

func TestChStatsWriter_Save_NilPreparer(t *testing.T) {
	w := clickhouse.NewChStatsWriterWithPreparer(nil)
	p := w.SaveStats(context.Background(), testStatsWindowClosed())
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for nil preparer, got=%v", p)
	}
}

func TestChStatsWriter_Save_FlushError(t *testing.T) {
	batch := &fakeBatch{
		flushErr: problem.Wrap(errors.New("timeout"), problem.Unavailable, "flush failed"),
	}
	w := clickhouse.NewChStatsWriterWithPreparer(&fakePreparer{batch: batch})

	p := w.SaveStats(context.Background(), testStatsWindowClosed())
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want=%q", p.Code, problem.Unavailable)
	}
}
