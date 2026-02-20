package insightsruntime

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"time"

	insightsapp "github.com/market-raccoon/internal/core/insights/app"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

func TestVPVROverloadSoakBurstDeterministicBudgets(t *testing.T) {
	if testing.Short() {
		t.Skip("skip soak in short mode")
	}

	runtime.GC()
	gBefore := runtime.NumGoroutine()
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	latA, emittedA, hashA := runVPVRSoakBurst(t)
	_, emittedB, hashB := runVPVRSoakBurst(t)

	runtime.GC()
	gAfter := runtime.NumGoroutine()
	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	if hashA != hashB {
		t.Fatalf("non-deterministic emitted sequence hash: %s vs %s", hashA, hashB)
	}
	assertVPVRSoakCoverage(t, emittedA, 4000)
	if !slices.Equal(emittedA, emittedB) {
		t.Fatalf("non-deterministic emitted sequence details: runA=%d runB=%d", len(emittedA), len(emittedB))
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

func runVPVRSoakBurst(t *testing.T) ([]time.Duration, []vpvrSoakEmission, string) {
	t.Helper()
	uc := insightsapp.NewBuildVolumeProfile()
	policy := insightsapp.NewVPVREmitPolicy(DefaultDecideFunc())
	latencies := make([]time.Duration, 0, 4000)
	emitted := make([]vpvrSoakEmission, 0, 4000)
	hashInput := make([]string, 0, 4000)

	for i := 1; i <= 4000; i++ {
		req := insightsapp.BuildVolumeProfileRequest{
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
		decision := policy.Apply(insightsapp.VPVROverloadInput{
			Venue:       req.Venue,
			Instrument:  req.Instrument,
			Timeframe:   req.Timeframe,
			Seq:         req.Seq,
			WindowClose: i%60 == 0,
			Signals: insightsapp.VPVROverloadSignals{
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
		appendVPVRSoakEmission(t, &emitted, &hashInput, req.Seq, "S", decision.EmitSnapshot, decision.Snapshot, i)
		appendVPVRSoakEmission(t, &emitted, &hashInput, req.Seq, "D", decision.EmitDelta, decision.Delta, i)
	}

	return latencies, emitted, sharedhash.HashFieldsFast(hashInput...)
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

func appendVPVRSoakEmission(
	t *testing.T,
	emitted *[]vpvrSoakEmission,
	hashInput *[]string,
	seq int64,
	kind string,
	emit bool,
	snapshot insightsdomain.VolumeProfileSnapshotV1,
	i int,
) {
	t.Helper()
	if !emit {
		return
	}
	raw, err := insightsapp.MarshalVPVRSnapshotStableBytes(snapshot)
	if err != nil {
		t.Fatalf("marshal %s i=%d: %v", kind, i, err)
	}
	out := vpvrSoakEmission{
		Kind: kind,
		Seq:  seq,
		Hash: sharedhash.HashBytes(raw),
	}
	*emitted = append(*emitted, out)
	*hashInput = append(*hashInput, out.Signature())
}

type vpvrSoakEmission struct {
	Kind string
	Seq  int64
	Hash string
}

func (e vpvrSoakEmission) Signature() string {
	return fmt.Sprintf("%s:%d:%s", e.Kind, e.Seq, e.Hash)
}

func assertVPVRSoakZeroDup(t *testing.T, emitted []vpvrSoakEmission) {
	t.Helper()
	seen := make(map[string]struct{}, len(emitted))
	for _, e := range emitted {
		sig := e.Signature()
		if _, dup := seen[sig]; dup {
			t.Fatalf("duplicate emitted signature detected: %s", sig)
		}
		seen[sig] = struct{}{}
	}
}

func assertVPVRSoakSeqMonotonic(t *testing.T, emitted []vpvrSoakEmission) {
	t.Helper()
	last := int64(0)
	for _, e := range emitted {
		if e.Seq < last {
			t.Fatalf("emitted sequence regressed: prev=%d current=%d", last, e.Seq)
		}
		last = e.Seq
	}
}

func assertVPVRSoakWindowCloseCoverage(t *testing.T, emitted []vpvrSoakEmission, totalEvents int, windowStride int64) {
	t.Helper()
	windowSnapshotSeq := make(map[int64]struct{}, totalEvents/int(windowStride))
	for _, e := range emitted {
		if e.Kind == "S" {
			windowSnapshotSeq[e.Seq] = struct{}{}
		}
	}
	for seq := windowStride; seq <= int64(totalEvents); seq += windowStride {
		if _, ok := windowSnapshotSeq[seq]; !ok {
			t.Fatalf("missing window-close snapshot emission at seq=%d", seq)
		}
	}
}

func assertVPVRSoakEmitCountBounds(t *testing.T, emitted []vpvrSoakEmission, totalEvents int) {
	t.Helper()
	minExpected := totalEvents / 60
	maxExpected := totalEvents * 2
	if got := len(emitted); got < minExpected || got > maxExpected {
		t.Fatalf("unexpected emitted count=%d bounds=[%d,%d]", got, minExpected, maxExpected)
	}
}

func assertVPVRSoakCoverage(t *testing.T, emitted []vpvrSoakEmission, totalEvents int) {
	t.Helper()
	if len(emitted) == 0 {
		t.Fatal("expected at least one emitted overload event")
	}
	assertVPVRSoakZeroDup(t, emitted)
	assertVPVRSoakSeqMonotonic(t, emitted)
	assertVPVRSoakWindowCloseCoverage(t, emitted, totalEvents, 60)
	assertVPVRSoakEmitCountBounds(t, emitted, totalEvents)
}
