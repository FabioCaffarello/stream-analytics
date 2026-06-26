package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/clickhouse"
	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func testChCandleClosed(timeframe string) aggdomain.CandleClosed {
	return aggdomain.CandleClosed{
		Candle: aggdomain.CandleV1{
			Venue:         "binance",
			Instrument:    "BTCUSDT",
			Timeframe:     timeframe,
			WindowStartTs: 1_710_000_000_000,
			WindowEndTs:   1_710_000_060_000,
			Open:          100.0,
			High:          101.0,
			Low:           99.0,
			ClosePrice:    100.5,
			Volume:        12.0,
			BuyVolume:     7.0,
			SellVolume:    5.0,
			TradeCount:    4,
			SeqFirst:      100,
			SeqLast:       103,
			IsClosed:      true,
		},
	}
}

func TestChCandleWriter_Save_Success(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChCandleWriterWithPreparer(&fakePreparer{batch: batch})

	if p := w.SaveCandle(context.Background(), testChCandleClosed("1m")); p != nil {
		t.Fatalf("save candle: %v", p)
	}
	if len(batch.rows) != 1 {
		t.Fatalf("rows=%d want=1", len(batch.rows))
	}
	// 16 columns: venue, instrument, timeframe, window_start, window_end,
	// open, high, low, close, volume, buy_volume, sell_volume, trade_count,
	// seq_first, seq_last, idempotency_key
	if len(batch.rows[0]) != 16 {
		t.Fatalf("columns=%d want=16", len(batch.rows[0]))
	}
	if batch.flushes != 1 {
		t.Fatalf("flushes=%d want=1", batch.flushes)
	}
}

func TestChCandleWriter_Save_NilPreparer(t *testing.T) {
	w := clickhouse.NewChCandleWriterWithPreparer(nil)
	p := w.SaveCandle(context.Background(), testChCandleClosed("1m"))
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed for nil preparer, got=%v", p)
	}
}

func TestChCandleWriter_Save_PrepareError(t *testing.T) {
	w := clickhouse.NewChCandleWriterWithPreparer(&fakePreparer{
		p: problem.Wrap(errors.New("conn"), problem.Unavailable, "prepare failed"),
	})
	p := w.SaveCandle(context.Background(), testChCandleClosed("1m"))
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want=%q", p.Code, problem.Unavailable)
	}
}

func TestChCandleWriter_Save_FlushError(t *testing.T) {
	batch := &fakeBatch{
		flushErr: problem.Wrap(errors.New("timeout"), problem.Unavailable, "flush failed"),
	}
	w := clickhouse.NewChCandleWriterWithPreparer(&fakePreparer{batch: batch})

	p := w.SaveCandle(context.Background(), testChCandleClosed("1m"))
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want=%q", p.Code, problem.Unavailable)
	}
}
