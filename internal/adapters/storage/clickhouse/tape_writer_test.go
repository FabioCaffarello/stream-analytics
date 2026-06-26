package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/clickhouse"
	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func testTapeClosed() aggdomain.TapeClosed {
	return aggdomain.TapeClosed{
		Window: aggdomain.TapeWindowV1{
			Venue:            "binance",
			Instrument:       "BTCUSDT",
			Timeframe:        "1s",
			WindowStartTs:    1_710_000_000_000,
			WindowEndTs:      1_710_000_001_000,
			TradeCount:       10,
			BuyCount:         6,
			SellCount:        4,
			BuyVolume:        3.5,
			SellVolume:       2.5,
			TotalVolume:      6.0,
			BuyNotional:      350_000,
			SellNotional:     250_000,
			VwapPrice:        100_000,
			MaxPrice:         100_100,
			MinPrice:         99_900,
			LastPrice:        100_050,
			MaxTradeSize:     1.2,
			RateTradesPerSec: 10.0,
			VolumeImbalance:  0.167,
			LastSeq:          42,
		},
		IsBurst: true,
	}
}

func TestChTapeWriter_Save_Success(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChTapeWriterWithPreparer(&fakePreparer{batch: batch})

	if p := w.SaveTape(context.Background(), testTapeClosed()); p != nil {
		t.Fatalf("save tape: %v", p)
	}
	if len(batch.rows) != 1 {
		t.Fatalf("rows=%d want=1", len(batch.rows))
	}
	if len(batch.rows[0]) != 23 {
		t.Fatalf("columns=%d want=23", len(batch.rows[0]))
	}
	if batch.flushes != 1 {
		t.Fatalf("flushes=%d want=1", batch.flushes)
	}
}

func TestChTapeWriter_Save_NilPreparer(t *testing.T) {
	w := clickhouse.NewChTapeWriterWithPreparer(nil)
	p := w.SaveTape(context.Background(), testTapeClosed())
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

func TestChTapeWriter_Save_PrepareError(t *testing.T) {
	w := clickhouse.NewChTapeWriterWithPreparer(&fakePreparer{
		p: problem.Wrap(errors.New("conn"), problem.Unavailable, "prepare failed"),
	})
	p := w.SaveTape(context.Background(), testTapeClosed())
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got=%v", p)
	}
}

func TestChTapeWriter_Save_FlushError(t *testing.T) {
	batch := &fakeBatch{
		flushErr: problem.Wrap(errors.New("timeout"), problem.Unavailable, "flush failed"),
	}
	w := clickhouse.NewChTapeWriterWithPreparer(&fakePreparer{batch: batch})
	p := w.SaveTape(context.Background(), testTapeClosed())
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got=%v", p)
	}
}
