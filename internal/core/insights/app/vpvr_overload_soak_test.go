package app

import (
	"context"
	"runtime"
	"slices"
	"testing"
	"time"

	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

func TestVPVROverloadSoakBurstDeterministicBudgets(t *testing.T) {
	if testing.Short() {
		t.Skip("skip soak in short mode")
	}

	run := func() ([]time.Duration, string) {
		uc := NewBuildVolumeProfile()
		policy := NewVPVREmitPolicy()
		latencies := make([]time.Duration, 0, 4000)
		emitted := make([]string, 0, 4000)

		for i := 1; i <= 4000; i++ {
			req := BuildVolumeProfileRequest{
				EventType:  "marketdata.trade",
				Venue:      "binance",
				Instrument: "BTC-USDT",
				Timeframe:  "1m",
				TickSize:   0.5,
				Price:      100 + float64(i%64)*0.5,
				Size:       1 + float64(i%7),
				Side:       sideByIndex(i),
				TsIngest:   1_710_000_000_000 + int64(i)*1_000,
				Seq:        int64(i),
			}
			res := uc.Execute(context.Background(), req)
			if res.IsFail() {
				t.Fatalf("build failed i=%d: %v", i, res.Problem())
			}
			out := res.Value()
			if !out.Emitted {
				continue
			}

			start := time.Now()
			decision := policy.Apply(VPVROverloadInput{
				Venue:       req.Venue,
				Instrument:  req.Instrument,
				Timeframe:   req.Timeframe,
				Seq:         req.Seq,
				WindowClose: i%60 == 0,
				Signals: VPVROverloadSignals{
					QueueDepth:          (i * 37) % 100,
					QueueCapacity:       100,
					BoundedMapOccupancy: (i * 11) % 100,
					BoundedMapLimit:     100,
					ProcessingLatencyMs: float64((i * 9) % 120),
				},
				Snapshot:     out.Snapshot,
				Delta:        out.Delta,
				HasDelta:     true,
				ProcessingMs: float64((i * 3) % 30),
			})
			latencies = append(latencies, time.Since(start))
			if decision.EmitSnapshot {
				raw, err := MarshalVPVRSnapshotStableBytes(decision.Snapshot)
				if err != nil {
					t.Fatalf("marshal snapshot i=%d: %v", i, err)
				}
				emitted = append(emitted, "S:"+sharedhash.HashBytes(raw))
			}
			if decision.EmitDelta {
				raw, err := MarshalVPVRSnapshotStableBytes(decision.Delta)
				if err != nil {
					t.Fatalf("marshal delta i=%d: %v", i, err)
				}
				emitted = append(emitted, "D:"+sharedhash.HashBytes(raw))
			}
		}
		return latencies, sharedhash.HashFields(emitted...)
	}

	runtime.GC()
	gBefore := runtime.NumGoroutine()
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	latA, hashA := run()
	_, hashB := run()

	runtime.GC()
	gAfter := runtime.NumGoroutine()
	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	if hashA != hashB {
		t.Fatalf("non-deterministic emitted sequence hash: %s vs %s", hashA, hashB)
	}

	p95 := durationPercentile(latA, 0.95)
	p99 := durationPercentile(latA, 0.99)
	if p95 > 2*time.Millisecond {
		t.Fatalf("p95=%s above budget=2ms", p95)
	}
	if p99 > 5*time.Millisecond {
		t.Fatalf("p99=%s above budget=5ms", p99)
	}

	if gAfter-gBefore > 16 {
		t.Fatalf("goroutine drift too high: before=%d after=%d", gBefore, gAfter)
	}
	if heapDeltaBytes(mBefore.HeapAlloc, mAfter.HeapAlloc) > 32*1024*1024 {
		t.Fatalf("heap drift too high: before=%d after=%d", mBefore.HeapAlloc, mAfter.HeapAlloc)
	}
}

func sideByIndex(i int) string {
	if i%2 == 0 {
		return "buy"
	}
	return "sell"
}

func durationPercentile(items []time.Duration, p float64) time.Duration {
	if len(items) == 0 {
		return 0
	}
	cp := append([]time.Duration(nil), items...)
	slices.Sort(cp)
	idx := int(float64(len(cp)-1) * p)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

func heapDeltaBytes(before, after uint64) uint64 {
	if after <= before {
		return 0
	}
	return after - before
}
