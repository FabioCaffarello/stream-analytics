package timescale_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/market-raccoon/internal/adapters/storage/timescale"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// fakeRows / fakeQuerier — generic fake for pgx multi-row queries
// ---------------------------------------------------------------------------

type fakeRow struct {
	values []any
}

type fakeRows struct {
	items  []fakeRow
	cols   []string
	idx    int
	closed bool
	err    error
}

func (r *fakeRows) Next() bool {
	if r == nil || r.closed {
		return false
	}
	return r.idx < len(r.items)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.idx >= len(r.items) {
		return errors.New("no more rows")
	}
	row := r.items[r.idx]
	r.idx++
	for i, d := range dest {
		if i >= len(row.values) {
			break
		}
		assignDest(d, row.values[i])
	}
	return nil
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }

func assignDest(dest, src any) {
	switch d := dest.(type) {
	case *string:
		if v, ok := src.(string); ok {
			*d = v
		}
	case *int64:
		if v, ok := src.(int64); ok {
			*d = v
		}
	case *float64:
		if v, ok := src.(float64); ok {
			*d = v
		}
	case **float64:
		if v, ok := src.(*float64); ok {
			*d = v
		}
		if v, ok := src.(float64); ok {
			*d = &v
		}
	case *bool:
		if v, ok := src.(bool); ok {
			*d = v
		}
	}
}

type fakeQuerier struct {
	rows     pgx.Rows
	queryErr error
}

func (q *fakeQuerier) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	if q.queryErr != nil {
		return nil, q.queryErr
	}
	return q.rows, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func float64Ptr(v float64) *float64 { return &v }

// ---------------------------------------------------------------------------
// CandleReader tests
// ---------------------------------------------------------------------------

func TestPgCandleReader_GetCandleRange_Success(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{items: []fakeRow{{
		values: []any{
			"binance", "BTCUSDT", "1m",
			int64(1_710_000_000_000), int64(1_710_000_060_000),
			float64(100_000), float64(100_500), float64(99_500), float64(100_200),
			float64(50.5), float64(30.0), float64(20.5),
			int64(100), int64(1), int64(42),
		},
	}}}}
	r := timescale.NewPgCandleReaderWithQuerier(q)
	candles, p := r.GetCandleRange(context.Background(), "binance", "BTCUSDT", "1m", 0, 9999999999999, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(candles) != 1 {
		t.Fatalf("got %d candles, want 1", len(candles))
	}
	c := candles[0]
	if c.Venue != "binance" || c.Instrument != "BTCUSDT" || c.Timeframe != "1m" {
		t.Fatalf("wrong key: %s/%s/%s", c.Venue, c.Instrument, c.Timeframe)
	}
	if c.Open != 100_000 || c.ClosePrice != 100_200 {
		t.Fatalf("wrong prices: open=%f close=%f", c.Open, c.ClosePrice)
	}
	if !c.IsClosed {
		t.Fatal("candle should be closed")
	}
}

func TestPgCandleReader_NilQuerier(t *testing.T) {
	r := timescale.NewPgCandleReaderWithQuerier(nil)
	_, p := r.GetCandleRange(context.Background(), "x", "x", "1m", 0, 1, 10)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

func TestPgCandleReader_QueryError(t *testing.T) {
	q := &fakeQuerier{queryErr: errors.New("conn refused")}
	r := timescale.NewPgCandleReaderWithQuerier(q)
	_, p := r.GetCandleRange(context.Background(), "x", "x", "1m", 0, 1, 10)
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got=%v", p)
	}
}

func TestPgCandleReader_GetCandleTimestamps_Success(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{items: []fakeRow{
		{values: []any{int64(1000)}},
		{values: []any{int64(2000)}},
	}}}
	r := timescale.NewPgCandleReaderWithQuerier(q)
	ts, p := r.GetCandleTimestamps(context.Background(), "binance", "BTCUSDT", "1m", 0, 9999999999999)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(ts) != 2 || ts[0] != 1000 || ts[1] != 2000 {
		t.Fatalf("got timestamps %v, want [1000 2000]", ts)
	}
}

func TestPgCandleReader_GetFirstCandle_Empty(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{}}
	r := timescale.NewPgCandleReaderWithQuerier(q)
	c, p := r.GetFirstCandle(context.Background(), "binance", "BTCUSDT", "1m")
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if c != nil {
		t.Fatal("expected nil candle for empty result")
	}
}

func TestPgCandleReader_GetLastCandle_Success(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{items: []fakeRow{{
		values: []any{
			"binance", "BTCUSDT", "1m",
			int64(5000), int64(6000),
			float64(50), float64(60), float64(40), float64(55),
			float64(10), float64(6), float64(4),
			int64(20), int64(1), int64(20),
		},
	}}}}
	r := timescale.NewPgCandleReaderWithQuerier(q)
	c, p := r.GetLastCandle(context.Background(), "binance", "BTCUSDT", "1m")
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if c == nil {
		t.Fatal("expected non-nil candle")
	}
	if c.WindowStartTs != 5000 {
		t.Fatalf("wrong window_start: %d", c.WindowStartTs)
	}
}

// ---------------------------------------------------------------------------
// StatsReader tests
// ---------------------------------------------------------------------------

func TestPgStatsReader_GetStatsRange_Success(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{items: []fakeRow{{
		values: []any{
			"binance", "BTCUSDT", "1m",
			int64(1000), int64(2000),
			float64(1.0), float64(2.0), float64(3.0), int64(5),
			float64(100.0), float64(110.0), float64(90.0), float64(105.0),
			float64(0.0001), float64(0.0002),
			int64(1), int64(10),
		},
	}}}}
	r := timescale.NewPgStatsReaderWithQuerier(q)
	stats, p := r.GetStatsRange(context.Background(), "binance", "BTCUSDT", "1m", 0, 9999999999999, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(stats) != 1 {
		t.Fatalf("got %d stats, want 1", len(stats))
	}
	if stats[0].LiqCount != 5 {
		t.Fatalf("wrong liq_count: %d", stats[0].LiqCount)
	}
}

func TestPgStatsReader_NilQuerier(t *testing.T) {
	r := timescale.NewPgStatsReaderWithQuerier(nil)
	_, p := r.GetStatsRange(context.Background(), "x", "x", "1m", 0, 1, 10)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

func TestPgStatsReader_GetStatsTimestamps_Success(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{items: []fakeRow{
		{values: []any{int64(100)}},
		{values: []any{int64(200)}},
	}}}
	r := timescale.NewPgStatsReaderWithQuerier(q)
	ts, p := r.GetStatsTimestamps(context.Background(), "binance", "BTCUSDT", "1m", 0, 9999999999999)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(ts) != 2 {
		t.Fatalf("got %d timestamps, want 2", len(ts))
	}
}

func TestPgStatsReader_GetFirstStats_Empty(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{}}
	r := timescale.NewPgStatsReaderWithQuerier(q)
	s, p := r.GetFirstStats(context.Background(), "binance", "BTCUSDT", "1m")
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if s != nil {
		t.Fatal("expected nil stats for empty result")
	}
}

// ---------------------------------------------------------------------------
// TapeReader tests
// ---------------------------------------------------------------------------

func TestPgTapeReader_GetTapeRange_Success(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{items: []fakeRow{{
		values: []any{
			"binance", "BTCUSDT", "1s",
			int64(1000), int64(2000),
			int64(10), int64(6), int64(4),
			float64(3.5), float64(2.5), float64(6.0),
			float64(350_000), float64(250_000),
			float64(100_000), float64(100_100), float64(99_900), float64(100_050),
			float64(1.2), float64(10.0), float64(0.167),
			int64(42),
		},
	}}}}
	r := timescale.NewPgTapeReaderWithQuerier(q)
	tapes, p := r.GetTapeRange(context.Background(), "binance", "BTCUSDT", "1s", 0, 9999999999999, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(tapes) != 1 {
		t.Fatalf("got %d tapes, want 1", len(tapes))
	}
	if tapes[0].TradeCount != 10 {
		t.Fatalf("wrong trade_count: %d", tapes[0].TradeCount)
	}
}

func TestPgTapeReader_NilQuerier(t *testing.T) {
	r := timescale.NewPgTapeReaderWithQuerier(nil)
	_, p := r.GetTapeRange(context.Background(), "x", "x", "1s", 0, 1, 10)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

func TestPgTapeReader_QueryError(t *testing.T) {
	q := &fakeQuerier{queryErr: errors.New("conn refused")}
	r := timescale.NewPgTapeReaderWithQuerier(q)
	_, p := r.GetTapeRange(context.Background(), "x", "x", "1s", 0, 1, 10)
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got=%v", p)
	}
}

// ---------------------------------------------------------------------------
// OIReader tests
// ---------------------------------------------------------------------------

func TestPgOIReader_GetOIRange_Success(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{items: []fakeRow{{
		values: []any{
			"binance", "BTCUSDT", "1m",
			int64(1000), int64(2000),
			float64(50000.0), float64(100.0), float64(0.2),
			int64(5), int64(1000),
		},
	}}}}
	r := timescale.NewPgOIReaderWithQuerier(q)
	rows, p := r.GetOIRange(context.Background(), "binance", "BTCUSDT", "1m", 0, 9999999999999, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].OpenInterest != 50000.0 {
		t.Fatalf("wrong open_interest: %f", rows[0].OpenInterest)
	}
}

func TestPgOIReader_NilQuerier(t *testing.T) {
	r := timescale.NewPgOIReaderWithQuerier(nil)
	_, p := r.GetOIRange(context.Background(), "x", "x", "1m", 0, 1, 10)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

// ---------------------------------------------------------------------------
// DeltaVolumeReader tests
// ---------------------------------------------------------------------------

func TestPgDeltaVolumeReader_GetDeltaVolumeRange_Success(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{items: []fakeRow{{
		values: []any{
			"binance", "BTCUSDT", "1m",
			int64(1000), int64(2000),
			float64(30.0), float64(20.0), float64(10.0),
			int64(5), int64(1000),
		},
	}}}}
	r := timescale.NewPgDeltaVolumeReaderWithQuerier(q)
	rows, p := r.GetDeltaVolumeRange(context.Background(), "binance", "BTCUSDT", "1m", 0, 9999999999999, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].DeltaVolume != 10.0 {
		t.Fatalf("wrong delta_volume: %f", rows[0].DeltaVolume)
	}
}

func TestPgDeltaVolumeReader_NilQuerier(t *testing.T) {
	r := timescale.NewPgDeltaVolumeReaderWithQuerier(nil)
	_, p := r.GetDeltaVolumeRange(context.Background(), "x", "x", "1m", 0, 1, 10)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

// ---------------------------------------------------------------------------
// CVDReader tests
// ---------------------------------------------------------------------------

func TestPgCVDReader_GetCVDRange_Success(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{items: []fakeRow{{
		values: []any{
			"binance", "BTCUSDT", "1m",
			int64(1000), int64(2000),
			float64(10.0), float64(150.0),
			int64(5), int64(1000),
		},
	}}}}
	r := timescale.NewPgCVDReaderWithQuerier(q)
	rows, p := r.GetCVDRange(context.Background(), "binance", "BTCUSDT", "1m", 0, 9999999999999, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].CVD != 150.0 {
		t.Fatalf("wrong cvd: %f", rows[0].CVD)
	}
}

func TestPgCVDReader_NilQuerier(t *testing.T) {
	r := timescale.NewPgCVDReaderWithQuerier(nil)
	_, p := r.GetCVDRange(context.Background(), "x", "x", "1m", 0, 1, 10)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

// ---------------------------------------------------------------------------
// BarStatsReader tests
// ---------------------------------------------------------------------------

func TestPgBarStatsReader_GetBarStatsRange_Success(t *testing.T) {
	q := &fakeQuerier{rows: &fakeRows{items: []fakeRow{{
		values: []any{
			"binance", "BTCUSDT", "1m",
			int64(1000), int64(2000),
			int64(10), int64(6), int64(4),
			float64(6.0), float64(3.5), float64(2.5),
			float64(100_000), float64(100_050), float64(100_100), float64(99_900),
			float64(0.167), bool(true), int64(5), int64(1000),
		},
	}}}}
	r := timescale.NewPgBarStatsReaderWithQuerier(q)
	rows, p := r.GetBarStatsRange(context.Background(), "binance", "BTCUSDT", "1m", 0, 9999999999999, 100)
	if p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	b := rows[0]
	if b.TradeCount != 10 {
		t.Fatalf("wrong trade_count: %d", b.TradeCount)
	}
	if !b.IsBurst {
		t.Fatal("expected is_burst=true")
	}
}

func TestPgBarStatsReader_NilQuerier(t *testing.T) {
	r := timescale.NewPgBarStatsReaderWithQuerier(nil)
	_, p := r.GetBarStatsRange(context.Background(), "x", "x", "1m", 0, 1, 10)
	if p == nil || p.Code != problem.ValidationFailed {
		t.Fatalf("expected ValidationFailed, got=%v", p)
	}
}

func TestPgBarStatsReader_QueryError(t *testing.T) {
	q := &fakeQuerier{queryErr: errors.New("conn refused")}
	r := timescale.NewPgBarStatsReaderWithQuerier(q)
	_, p := r.GetBarStatsRange(context.Background(), "x", "x", "1m", 0, 1, 10)
	if p == nil || p.Code != problem.Unavailable {
		t.Fatalf("expected Unavailable, got=%v", p)
	}
}

// suppress unused imports
var (
	_ = aggdomain.CandleV1{}
	_ = float64Ptr
)
