package timescale_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	"github.com/market-raccoon/internal/adapters/storage/timescale"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

type fakeSQLExecutor struct {
	lastQuery string
	lastArgs  []any
	rows      int64
	p         *problem.Problem
}

func (f *fakeSQLExecutor) Exec(_ context.Context, query string, args ...any) (int64, *problem.Problem) {
	f.lastQuery = query
	f.lastArgs = append([]any(nil), args...)
	return f.rows, f.p
}

func (f *fakeSQLExecutor) QueryRow(context.Context, string, ...any) adapterstorage.Row { return nil }

func TestPgWriter_Save_Success(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgWriterWithExecutor(exec)

	if p := w.Save(context.Background(), testPgSnapshot(42)); p != nil {
		t.Fatalf("save: %v", p)
	}
	if !strings.Contains(exec.lastQuery, "aggregation_orderbook_snapshot") {
		t.Fatalf("query=%q missing target table", exec.lastQuery)
	}
	if len(exec.lastArgs) != 5 {
		t.Fatalf("args len=%d want=5", len(exec.lastArgs))
	}
}

func TestPgWriter_Save_DuplicateIdempotent(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 0}
	w := timescale.NewPgWriterWithExecutor(exec)

	if p := w.Save(context.Background(), testPgSnapshot(42)); p != nil {
		t.Fatalf("save: %v", p)
	}
}

func TestPgWriter_Save_ConnectionError(t *testing.T) {
	exec := &fakeSQLExecutor{
		p: problem.Wrap(errors.New("db down"), problem.Unavailable, "timescale exec failed"),
	}
	w := timescale.NewPgWriterWithExecutor(exec)

	p := w.Save(context.Background(), testPgSnapshot(42))
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want=%q", p.Code, problem.Unavailable)
	}
}

func testPgSnapshot(seq int64) aggdomain.SnapshotProduced {
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
