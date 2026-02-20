package timescale

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/market-raccoon/internal/core/delivery/domain"
)

// ---------------------------------------------------------------------------
// fakeRow / fakeDeliveryRows / fakeDeliveryQuerier
// ---------------------------------------------------------------------------

type fakeDeliveryRow struct {
	seq      int64
	tsIngest int64
	payload  []byte
}

type fakeDeliveryRows struct {
	items []fakeDeliveryRow
	idx   int
	err   error
}

func (r *fakeDeliveryRows) Next() bool {
	return r.idx < len(r.items)
}

func (r *fakeDeliveryRows) Scan(dest ...any) error {
	if r.idx >= len(r.items) {
		return errors.New("no more rows")
	}
	row := r.items[r.idx]
	r.idx++
	if len(dest) >= 1 {
		if p, ok := dest[0].(*int64); ok {
			*p = row.seq
		}
	}
	if len(dest) >= 2 {
		if p, ok := dest[1].(*int64); ok {
			*p = row.tsIngest
		}
	}
	if len(dest) >= 3 {
		if p, ok := dest[2].(*[]byte); ok {
			*p = append([]byte(nil), row.payload...)
		}
	}
	return nil
}

func (r *fakeDeliveryRows) Close()                                       {}
func (r *fakeDeliveryRows) Err() error                                   { return r.err }
func (r *fakeDeliveryRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeDeliveryRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeDeliveryRows) RawValues() [][]byte                          { return nil }
func (r *fakeDeliveryRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeDeliveryRows) Values() ([]any, error)                       { return nil, nil }

type fakeDeliveryQuerier struct {
	rows     []fakeDeliveryRow
	queryErr error
}

func (q *fakeDeliveryQuerier) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	if q.queryErr != nil {
		return nil, q.queryErr
	}
	return &fakeDeliveryRows{items: q.rows}, nil
}

func (q *fakeDeliveryQuerier) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPgRangeStore_QueryGetRange_ReturnsItems(t *testing.T) {
	q := &fakeDeliveryQuerier{
		rows: []fakeDeliveryRow{
			{seq: 1, tsIngest: 100, payload: []byte("a")},
			{seq: 2, tsIngest: 200, payload: []byte("b")},
			{seq: 3, tsIngest: 300, payload: []byte("c")},
		},
	}
	store := NewPgRangeStoreWithQuerier(q, 4096)

	sub, p := domain.ParseSubject("aggregation.snapshot/binance/BTCUSDT/raw")
	if p != nil {
		t.Fatalf("ParseSubject: %v", p)
	}

	items, pp := store.GetRange(context.Background(), sub, 0, 0, 10)
	if pp != nil {
		t.Fatalf("GetRange: %v", pp)
	}
	if got := len(items); got != 3 {
		t.Fatalf("items len=%d want=3", got)
	}
	if items[0].Seq != 1 || items[1].Seq != 2 || items[2].Seq != 3 {
		t.Fatalf("unexpected seq values: %d, %d, %d", items[0].Seq, items[1].Seq, items[2].Seq)
	}
}

func TestPgRangeStore_QueryGetRange_EmptyResult(t *testing.T) {
	q := &fakeDeliveryQuerier{rows: nil}
	store := NewPgRangeStoreWithQuerier(q, 4096)

	sub, p := domain.ParseSubject("aggregation.snapshot/binance/BTCUSDT/raw")
	if p != nil {
		t.Fatalf("ParseSubject: %v", p)
	}

	items, pp := store.GetRange(context.Background(), sub, 0, 0, 10)
	if pp != nil {
		t.Fatalf("GetRange: %v", pp)
	}
	if len(items) != 0 {
		t.Fatalf("items len=%d want=0", len(items))
	}
}

func TestPgRangeStore_QueryGetRange_LimitClamp(t *testing.T) {
	q := &fakeDeliveryQuerier{
		rows: []fakeDeliveryRow{
			{seq: 1, tsIngest: 100, payload: []byte("a")},
			{seq: 2, tsIngest: 200, payload: []byte("b")},
		},
	}
	store := NewPgRangeStoreWithQuerier(q, 4096)

	sub, p := domain.ParseSubject("aggregation.snapshot/binance/BTCUSDT/raw")
	if p != nil {
		t.Fatalf("ParseSubject: %v", p)
	}

	// limit=0 should be clamped to maxPerSubject
	items, pp := store.GetRange(context.Background(), sub, 0, 0, 0)
	if pp != nil {
		t.Fatalf("GetRange: %v", pp)
	}
	if len(items) != 2 {
		t.Fatalf("items len=%d want=2", len(items))
	}
}

func TestPgRangeStore_QueryGetRange_NilQuerier_ReturnsNil(t *testing.T) {
	store := NewPgRangeStoreWithQuerier(nil, 4096)

	sub, p := domain.ParseSubject("aggregation.snapshot/binance/BTCUSDT/raw")
	if p != nil {
		t.Fatalf("ParseSubject: %v", p)
	}

	items, pp := store.GetRange(context.Background(), sub, 0, 0, 10)
	if pp != nil {
		t.Fatalf("GetRange should not return problem for nil querier, got: %v", pp)
	}
	if len(items) != 0 {
		t.Fatalf("items len=%d want=0", len(items))
	}
}

func TestPgRangeStore_QueryGetRange_QueryError_GracefulDegradation(t *testing.T) {
	q := &fakeDeliveryQuerier{queryErr: errors.New("connection refused")}
	store := NewPgRangeStoreWithQuerier(q, 4096)

	sub, p := domain.ParseSubject("aggregation.snapshot/binance/BTCUSDT/raw")
	if p != nil {
		t.Fatalf("ParseSubject: %v", p)
	}

	// PgRangeStore.GetRange returns nil, nil on query error (graceful degradation).
	items, pp := store.GetRange(context.Background(), sub, 0, 0, 10)
	if pp != nil {
		t.Fatalf("expected graceful degradation, got problem: %v", pp)
	}
	if len(items) != 0 {
		t.Fatalf("items len=%d want=0", len(items))
	}
}
