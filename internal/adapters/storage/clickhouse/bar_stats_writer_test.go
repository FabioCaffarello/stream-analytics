package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/clickhouse"
	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func testBarStatsClosed() aggdomain.BarStatsClosed {
	return aggdomain.BarStatsClosed{
		Window: aggdomain.BarStatsWindowV1{
			Venue:         "binance",
			Instrument:    "BTCUSDT",
			Timeframe:     "1s",
			WindowStartTs: 1_710_000_000_000,
			WindowEndTs:   1_710_000_001_000,
			TradeCount:    10,
			BuyCount:      6,
			SellCount:     4,
			TotalVolume:   6.0,
			BuyVolume:     3.5,
			SellVolume:    2.5,
			VwapPrice:     100_000,
			LastPrice:     100_050,
			MaxPrice:      100_100,
			MinPrice:      99_900,
			Imbalance:     0.167,
			IsBurst:       true,
			Seq:           42,
			TsIngestMs:    1_710_000_001_000,
		},
	}
}

func TestChBarStatsWriter_Save_Success(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChBarStatsWriterWithPreparer(&fakePreparer{batch: batch})

	if p := w.SaveBarStats(context.Background(), testBarStatsClosed()); p != nil {
		t.Fatalf("save bar_stats: %v", p)
	}
	if len(batch.rows) != 1 {
		t.Fatalf("rows=%d want=1", len(batch.rows))
	}
	if len(batch.rows[0]) != 20 {
		t.Fatalf("columns=%d want=20", len(batch.rows[0]))
	}
}

func TestChBarStatsWriter_Save_NilPreparer(t *testing.T) {
	w := clickhouse.NewChBarStatsWriterWithPreparer(nil)
	p := w.SaveBarStats(context.Background(), testBarStatsClosed())
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

func TestChBarStatsWriter_Save_FlushError(t *testing.T) {
	batch := &fakeBatch{
		flushErr: problem.Wrap(errors.New("timeout"), problem.Unavailable, "flush failed"),
	}
	w := clickhouse.NewChBarStatsWriterWithPreparer(&fakePreparer{batch: batch})
	p := w.SaveBarStats(context.Background(), testBarStatsClosed())
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got=%v", p)
	}
}
