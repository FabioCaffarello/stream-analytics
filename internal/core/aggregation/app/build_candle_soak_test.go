//go:build soak
// +build soak

package app_test

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/market-raccoon/internal/core/aggregation/app"
)

func TestBuildCandle_Soak_HighCardinality(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}

	uc, _, _ := newCandleUC(1_000)
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < 2_000; i++ {
		_, p := uc.Execute(context.Background(), app.BuildCandleRequest{
			Venue:      "binance",
			Instrument: fmt.Sprintf("SYM%04dUSDT", i),
			Price:      100.0 + float64(i%10),
			Quantity:   1.0,
			IsBuy:      i%2 == 0,
			Seq:        1,
			TsIngest:   1,
		})
		if p != nil {
			t.Fatalf("Execute[%d]: %v", i, p)
		}
	}

	if got := uc.ActiveCandles(); got > 1_000 {
		t.Fatalf("active candles=%d exceeded max=1000", got)
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	var delta uint64
	if after.Alloc > before.Alloc {
		delta = after.Alloc - before.Alloc
	}
	if delta > 128*1024*1024 {
		t.Fatalf("unexpected alloc growth delta=%d bytes", delta)
	}
}

func TestBuildCandle_Soak_RapidWindowRolls(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}

	uc, pub, _ := newCandleUC(128)
	for i := 0; i < 10_000; i++ {
		_, p := uc.Execute(context.Background(), app.BuildCandleRequest{
			Venue:      "binance",
			Instrument: "BTCUSDT",
			Price:      100.0 + float64(i%100)/10.0,
			Quantity:   1.0,
			IsBuy:      i%2 == 0,
			Seq:        int64(i + 1),
			TsIngest:   int64(i*60_000 + 1),
		})
		if p != nil {
			t.Fatalf("Execute[%d]: %v", i, p)
		}
	}

	if got := uc.ActiveCandles(); got > 128 {
		t.Fatalf("active candles=%d exceeded max=128", got)
	}
	if len(pub.candles) == 0 {
		t.Fatal("expected closed candles during rapid window rolls")
	}
}
