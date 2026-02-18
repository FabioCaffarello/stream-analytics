package clickhouse_test

import (
	"context"
	"errors"
	"testing"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

type fakeBatch struct {
	rows      [][]any
	flushes   int
	appendErr *problem.Problem
	flushErr  *problem.Problem
}

func (b *fakeBatch) AppendRow(_ context.Context, values ...any) *problem.Problem {
	if b.appendErr != nil {
		return b.appendErr
	}
	b.rows = append(b.rows, append([]any(nil), values...))
	return nil
}

func (b *fakeBatch) Flush(context.Context) (int64, *problem.Problem) {
	b.flushes++
	if b.flushErr != nil {
		return 0, b.flushErr
	}
	return int64(len(b.rows)), nil
}

func (b *fakeBatch) Close() *problem.Problem { return nil }

type fakePreparer struct {
	batch *fakeBatch
	p     *problem.Problem
}

func (f *fakePreparer) PrepareInsert(context.Context, string) (adapterstorage.BatchInserter, *problem.Problem) {
	if f.p != nil {
		return nil, f.p
	}
	return f.batch, nil
}

func TestChWriter_Save_Success(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChWriterWithPreparer(&fakePreparer{batch: batch})

	if p := w.Save(context.Background(), testChSnapshot(42)); p != nil {
		t.Fatalf("save: %v", p)
	}
	if len(batch.rows) != 1 {
		t.Fatalf("rows=%d want=1", len(batch.rows))
	}
}

func TestChWriter_Save_BatchFlush(t *testing.T) {
	batch := &fakeBatch{}
	w := clickhouse.NewChWriterWithPreparer(&fakePreparer{batch: batch})

	if p := w.SaveIdempotent(context.Background(), testChSnapshot(42), "idem-key"); p != nil {
		t.Fatalf("save idempotent: %v", p)
	}
	if batch.flushes != 1 {
		t.Fatalf("flushes=%d want=1", batch.flushes)
	}
}

func TestChWriter_Save_ConnectionError(t *testing.T) {
	w := clickhouse.NewChWriterWithPreparer(&fakePreparer{
		p: problem.Wrap(errors.New("conn"), problem.Unavailable, "clickhouse prepare batch failed"),
	})
	p := w.Save(context.Background(), testChSnapshot(42))
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want=%q", p.Code, problem.Unavailable)
	}
}

func testChSnapshot(seq int64) aggdomain.SnapshotProduced {
	return aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{
			Venue:      "binance",
			Instrument: "BTCUSDT",
		},
		Seq: seq,
		Bids: []aggdomain.Level{{
			Price:    100,
			Quantity: 1,
		}},
		Asks: []aggdomain.Level{{
			Price:    101,
			Quantity: 1,
		}},
	}
}
