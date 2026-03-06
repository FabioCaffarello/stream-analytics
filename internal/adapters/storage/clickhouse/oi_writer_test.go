package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func testOIClosed() aggdomain.OpenInterestClosed {
	return aggdomain.OpenInterestClosed{
		Window: aggdomain.OpenInterestWindowV1{
			Venue:        "binance",
			Instrument:   "BTCUSDT",
			Timeframe:    "raw",
			WindowStartTs: 1_710_000_000_000,
			WindowEndTs:   1_710_000_000_000,
			OpenInterest: 15000.0,
			Delta:        200.0,
			DeltaPct:     0.0135,
			Seq:          42,
			TsIngestMs:   1_710_000_000_100,
		},
	}
}

func TestChOIWriter_Save_Success(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChOIWriterWithPreparer(&fakePreparer{batch: batch})

	if p := w.SaveOI(context.Background(), testOIClosed()); p != nil {
		t.Fatalf("save oi: %v", p)
	}
	if len(batch.rows) != 1 {
		t.Fatalf("rows=%d want=1", len(batch.rows))
	}
	if len(batch.rows[0]) != 11 {
		t.Fatalf("columns=%d want=11", len(batch.rows[0]))
	}
}

func TestChOIWriter_Save_NilPreparer(t *testing.T) {
	w := clickhouse.NewChOIWriterWithPreparer(nil)
	p := w.SaveOI(context.Background(), testOIClosed())
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

func TestChOIWriter_Save_PrepareError(t *testing.T) {
	w := clickhouse.NewChOIWriterWithPreparer(&fakePreparer{
		p: problem.Wrap(errors.New("conn"), problem.Unavailable, "prepare failed"),
	})
	p := w.SaveOI(context.Background(), testOIClosed())
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got=%v", p)
	}
}
