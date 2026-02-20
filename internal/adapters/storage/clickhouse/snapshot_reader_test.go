package clickhouse

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/shared/problem"
)

type fakeSnapshotRows struct {
	values      []int64
	idx         int
	current     int64
	scanCalls   int
	scanErrAt   int
	scanErr     error
	rowsErr     error
	closeCalled bool
}

func (r *fakeSnapshotRows) Next() bool {
	if r.idx >= len(r.values) {
		return false
	}
	r.current = r.values[r.idx]
	r.idx++
	return true
}

func (r *fakeSnapshotRows) Scan(dest ...any) error {
	r.scanCalls++
	if r.scanErrAt > 0 && r.scanCalls == r.scanErrAt {
		if r.scanErr != nil {
			return r.scanErr
		}
		return errors.New("scan failed")
	}
	ptr, ok := dest[0].(*int64)
	if !ok {
		return errors.New("dest must be *int64")
	}
	*ptr = r.current
	return nil
}

func (r *fakeSnapshotRows) Close() error {
	r.closeCalled = true
	return nil
}

func (r *fakeSnapshotRows) Err() error {
	return r.rowsErr
}

type fakeSnapshotQueryer struct {
	rows      snapshotRows
	queryErr  error
	lastQuery string
	lastArgs  []any
}

func (q *fakeSnapshotQueryer) Query(_ context.Context, query string, args ...any) (snapshotRows, error) {
	q.lastQuery = query
	q.lastArgs = append([]any(nil), args...)
	if q.queryErr != nil {
		return nil, q.queryErr
	}
	return q.rows, nil
}

func TestChSnapshotReader_GetSnapshotTimestamps_DeduplicatesAndBindsArgs(t *testing.T) {
	rows := &fakeSnapshotRows{values: []int64{1000, 1000, 2000, 3000, 3000}}
	q := &fakeSnapshotQueryer{rows: rows}
	r := NewChSnapshotReaderWithQueryer(q)

	got, p := r.GetSnapshotTimestamps(context.Background(), "binance", "BTCUSDT", 1000, 5000)
	if p != nil {
		t.Fatalf("GetSnapshotTimestamps: %v", p)
	}
	want := []int64{1000, 2000, 3000}
	if len(got) != len(want) {
		t.Fatalf("len=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%d want=%d", i, got[i], want[i])
		}
	}
	if !rows.closeCalled {
		t.Fatal("rows should be closed")
	}
	if !strings.Contains(q.lastQuery, "aggregation_orderbook_snapshot_cold FINAL") {
		t.Fatalf("query not targeting snapshot cold table: %q", q.lastQuery)
	}
	if len(q.lastArgs) != 4 {
		t.Fatalf("args=%d want=4", len(q.lastArgs))
	}
}

func TestChSnapshotReader_GetSnapshotTimestamps_NilReader(t *testing.T) {
	var r *ChSnapshotReader
	_, p := r.GetSnapshotTimestamps(context.Background(), "binance", "BTCUSDT", 0, 1)
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.ValidationFailed {
		t.Fatalf("code=%q want=%q", p.Code, problem.ValidationFailed)
	}
}

func TestChSnapshotReader_GetSnapshotTimestamps_QueryError(t *testing.T) {
	r := NewChSnapshotReaderWithQueryer(&fakeSnapshotQueryer{
		queryErr: errors.New("query down"),
	})
	_, p := r.GetSnapshotTimestamps(context.Background(), "binance", "BTCUSDT", 0, 1)
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want=%q", p.Code, problem.Unavailable)
	}
}

func TestChSnapshotReader_GetSnapshotTimestamps_ScanError(t *testing.T) {
	r := NewChSnapshotReaderWithQueryer(&fakeSnapshotQueryer{
		rows: &fakeSnapshotRows{
			values:    []int64{1000},
			scanErrAt: 1,
			scanErr:   errors.New("bad row"),
		},
	})
	_, p := r.GetSnapshotTimestamps(context.Background(), "binance", "BTCUSDT", 0, 1)
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.Internal {
		t.Fatalf("code=%q want=%q", p.Code, problem.Internal)
	}
}

func TestChSnapshotReader_GetSnapshotTimestamps_RowsError(t *testing.T) {
	r := NewChSnapshotReaderWithQueryer(&fakeSnapshotQueryer{
		rows: &fakeSnapshotRows{
			values:  []int64{1000},
			rowsErr: errors.New("rows failed"),
		},
	})
	_, p := r.GetSnapshotTimestamps(context.Background(), "binance", "BTCUSDT", 0, 1)
	if p == nil {
		t.Fatal("expected problem, got nil")
	}
	if p.Code != problem.Unavailable {
		t.Fatalf("code=%q want=%q", p.Code, problem.Unavailable)
	}
}
