package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/adapters/storage/clickhouse"
	aggdomain "github.com/FabioCaffarello/stream-analytics/internal/core/aggregation/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

func testCVDClosed() aggdomain.CVDClosed {
	return aggdomain.CVDClosed{
		Window: aggdomain.CVDWindowV1{
			Venue:         "binance",
			Instrument:    "BTCUSDT",
			Timeframe:     "1s",
			WindowStartTs: 1_710_000_000_000,
			WindowEndTs:   1_710_000_001_000,
			DeltaVolume:   1.0,
			CVD:           42.5,
			Seq:           42,
			TsIngestMs:    1_710_000_001_000,
		},
	}
}

func TestChCVDWriter_Save_Success(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChCVDWriterWithPreparer(&fakePreparer{batch: batch})

	if p := w.SaveCVD(context.Background(), testCVDClosed()); p != nil {
		t.Fatalf("save cvd: %v", p)
	}
	if len(batch.rows) != 1 {
		t.Fatalf("rows=%d want=1", len(batch.rows))
	}
	if len(batch.rows[0]) != 10 {
		t.Fatalf("columns=%d want=10", len(batch.rows[0]))
	}
}

func TestChCVDWriter_Save_NilPreparer(t *testing.T) {
	w := clickhouse.NewChCVDWriterWithPreparer(nil)
	p := w.SaveCVD(context.Background(), testCVDClosed())
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

func TestChCVDWriter_Save_FlushError(t *testing.T) {
	batch := &fakeBatch{
		flushErr: problem.Wrap(errors.New("timeout"), problem.Unavailable, "flush failed"),
	}
	w := clickhouse.NewChCVDWriterWithPreparer(&fakePreparer{batch: batch})
	p := w.SaveCVD(context.Background(), testCVDClosed())
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got=%v", p)
	}
}
