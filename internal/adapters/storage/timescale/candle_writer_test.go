package timescale_test

import (
	"context"
	"strings"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage/timescale"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
)

func TestPgCandleWriter_Save_Success(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 1}
	w := timescale.NewPgCandleWriterWithExecutor(exec)

	if p := w.SaveCandle(context.Background(), testCandleClosed("1m")); p != nil {
		t.Fatalf("save candle: %v", p)
	}
	if !strings.Contains(exec.lastQuery, "aggregation_candle") {
		t.Fatalf("query=%q missing target table", exec.lastQuery)
	}
	if len(exec.lastArgs) != 16 {
		t.Fatalf("args len=%d want=16", len(exec.lastArgs))
	}
}

func TestPgCandleWriter_Save_DuplicateIdempotent(t *testing.T) {
	exec := &fakeSQLExecutor{rows: 0}
	w := timescale.NewPgCandleWriterWithExecutor(exec)

	if p := w.SaveCandle(context.Background(), testCandleClosed("1m")); p != nil {
		t.Fatalf("save candle: %v", p)
	}
}

func TestPgCandleWriter_Save_AllTimeframes(t *testing.T) {
	timeframes := []string{"1m", "5m", "15m", "30m", "1h"}
	for _, tf := range timeframes {
		exec := &fakeSQLExecutor{rows: 1}
		w := timescale.NewPgCandleWriterWithExecutor(exec)
		if p := w.SaveCandle(context.Background(), testCandleClosed(tf)); p != nil {
			t.Fatalf("save candle tf=%s: %v", tf, p)
		}
		if got := exec.lastArgs[2]; got != tf {
			t.Fatalf("timeframe arg=%v want=%s", got, tf)
		}
	}
}

func testCandleClosed(timeframe string) aggdomain.CandleClosed {
	return aggdomain.CandleClosed{
		Candle: aggdomain.CandleV1{
			Venue:         "binance",
			Instrument:    "BTCUSDT",
			Timeframe:     timeframe,
			WindowStartTs: 1_710_000_000_000,
			WindowEndTs:   1_710_000_060_000,
			Open:          100.0,
			High:          101.0,
			Low:           99.0,
			ClosePrice:    100.5,
			Volume:        12.0,
			BuyVolume:     7.0,
			SellVolume:    5.0,
			TradeCount:    4,
			SeqFirst:      100,
			SeqLast:       103,
			IsClosed:      true,
		},
	}
}
