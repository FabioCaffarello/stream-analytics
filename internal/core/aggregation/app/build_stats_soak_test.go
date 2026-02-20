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

func TestBuildStats_Soak_HighCardinality(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soak test in short mode")
	}

	uc, _, _ := newStatsUC(1_000)
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	for i := 0; i < 2_000; i++ {
		_, p := uc.Execute(context.Background(), app.BuildStatsRequest{
			Venue:      "binance",
			Instrument: fmt.Sprintf("SYM%04dUSDT", i),
			Kind:       app.StatsInputMarkPrice,
			MarkPrice:  100.0 + float64(i%10),
			Seq:        1,
			TsIngest:   1,
		})
		if p != nil {
			t.Fatalf("Execute[%d]: %v", i, p)
		}
	}

	if got := uc.ActiveWindows(); got > 1_000 {
		t.Fatalf("active windows=%d exceeded max=1000", got)
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
