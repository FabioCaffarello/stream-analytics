package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

func testDeltaVolumeClosed() aggdomain.DeltaVolumeClosed {
	return aggdomain.DeltaVolumeClosed{
		Window: aggdomain.DeltaVolumeWindowV1{
			Venue:         "binance",
			Instrument:    "BTCUSDT",
			Timeframe:     "1s",
			WindowStartTs: 1_710_000_000_000,
			WindowEndTs:   1_710_000_001_000,
			BuyVolume:     3.5,
			SellVolume:    2.5,
			DeltaVolume:   1.0,
			Seq:           42,
			TsIngestMs:    1_710_000_001_000,
		},
	}
}

func TestChDeltaVolumeWriter_Save_Success(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChDeltaVolumeWriterWithPreparer(&fakePreparer{batch: batch})

	if p := w.SaveDeltaVolume(context.Background(), testDeltaVolumeClosed()); p != nil {
		t.Fatalf("save delta_volume: %v", p)
	}
	if len(batch.rows) != 1 {
		t.Fatalf("rows=%d want=1", len(batch.rows))
	}
	if len(batch.rows[0]) != 11 {
		t.Fatalf("columns=%d want=11", len(batch.rows[0]))
	}
}

func TestChDeltaVolumeWriter_Save_NilPreparer(t *testing.T) {
	w := clickhouse.NewChDeltaVolumeWriterWithPreparer(nil)
	p := w.SaveDeltaVolume(context.Background(), testDeltaVolumeClosed())
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

func TestChDeltaVolumeWriter_Save_FlushError(t *testing.T) {
	batch := &fakeBatch{
		flushErr: problem.Wrap(errors.New("timeout"), problem.Unavailable, "flush failed"),
	}
	w := clickhouse.NewChDeltaVolumeWriterWithPreparer(&fakePreparer{batch: batch})
	p := w.SaveDeltaVolume(context.Background(), testDeltaVolumeClosed())
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got=%v", p)
	}
}
