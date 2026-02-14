package storage_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/market-raccoon/internal/adapters/storage"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	insightsapp "github.com/market-raccoon/internal/core/insights/app"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/problem"
)

type commitRecorder struct {
	hotCommitted  map[int64]bool
	coldCommitted map[int64]bool
	ackCommitted  map[int64]bool
	order         []string
}

type commitRecorderHot struct{ rec *commitRecorder }
type commitRecorderCold struct{ rec *commitRecorder }
type integrationRunResult struct {
	emitted     []string
	order       []string
	windowFinal []string
}

func newCommitRecorder() *commitRecorder {
	return &commitRecorder{
		hotCommitted:  make(map[int64]bool),
		coldCommitted: make(map[int64]bool),
		ackCommitted:  make(map[int64]bool),
	}
}

func (w commitRecorderHot) Save(_ context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	w.rec.hotCommitted[snap.Seq] = true
	w.rec.order = append(w.rec.order, fmt.Sprintf("hot:%d", snap.Seq))
	return nil
}

func (w commitRecorderCold) Save(_ context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	w.rec.coldCommitted[snap.Seq] = true
	w.rec.order = append(w.rec.order, fmt.Sprintf("cold:%d", snap.Seq))
	return nil
}

func (r *commitRecorder) ackFn(seq int64) func() error {
	return func() error {
		if !r.hotCommitted[seq] || !r.coldCommitted[seq] {
			return fmt.Errorf("ack before commit seq=%d hot=%v cold=%v", seq, r.hotCommitted[seq], r.coldCommitted[seq])
		}
		r.ackCommitted[seq] = true
		r.order = append(r.order, fmt.Sprintf("ack:%d", seq))
		return nil
	}
}

func TestIntegrationVPVROverload_AckBoundarySafeAndDeterministic(t *testing.T) {
	t.Parallel()

	a := runVPVROverloadIntegration(t)
	b := runVPVROverloadIntegration(t)
	assertRunDeterministic(t, a, b)
}

func runVPVROverloadIntegration(t *testing.T) integrationRunResult {
	t.Helper()
	uc := insightsapp.NewBuildVolumeProfile()
	policy := insightsapp.NewVPVREmitPolicy()
	rec := newCommitRecorder()
	committer := storage.NewSnapshotCommitter(commitRecorderHot{rec: rec}, commitRecorderCold{rec: rec})

	emitted := make([]string, 0, 1024)
	windowFinal := make([]string, 0, 8)
	sawL2 := false
	sawL3 := false
	windowCloseCount := int64(0)

	for i := 1; i <= 240; i++ {
		req := buildVPVRRequest(i)
		res := uc.Execute(context.Background(), req)
		if res.IsFail() {
			t.Fatalf("build failed i=%d: %v", i, res.Problem())
		}
		out := res.Value()
		if !out.Emitted {
			continue
		}
		decision := applyOverloadDecision(policy, req, out, i)
		sawL2 = sawL2 || decision.Level == insightsapp.VPVROverloadL2
		sawL3 = sawL3 || decision.Level == insightsapp.VPVROverloadL3
		appendEmitSignatures(t, &emitted, req.Seq, decision)
		if i%60 != 0 {
			continue
		}
		windowCloseCount++
		windowHash := assertWindowCloseInvariant(t, i, out.Snapshot, decision.Snapshot, decision.EmitSnapshot)
		windowFinal = append(windowFinal, windowHash)
		commitWindowAndAck(t, committer, rec, windowCloseCount)
	}

	assertOverloadLevelsSeen(t, sawL2, sawL3)
	assertAckBoundary(t, rec, windowCloseCount)
	return integrationRunResult{emitted: emitted, order: rec.order, windowFinal: windowFinal}
}

func buildVPVRRequest(i int) insightsapp.BuildVolumeProfileRequest {
	side := "buy"
	if i%2 != 0 {
		side = "sell"
	}
	return insightsapp.BuildVolumeProfileRequest{
		EventType:  "marketdata.trade",
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Timeframe:  "1m",
		TickSize:   0.5,
		Price:      100 + float64(i%40)*0.5,
		Size:       1 + float64(i%5),
		Side:       side,
		TsIngest:   1_710_000_000_000 + int64(i)*1_000,
		Seq:        int64(i),
	}
}

func applyOverloadDecision(policy *insightsapp.VPVREmitPolicy, req insightsapp.BuildVolumeProfileRequest, out insightsapp.BuildVolumeProfileResponse, i int) insightsapp.VPVROverloadOutput {
	queueDepth := 82
	latency := 50.0
	if i%2 == 0 {
		queueDepth = 95
		latency = 100
	}
	return policy.Apply(insightsapp.VPVROverloadInput{
		Venue:       req.Venue,
		Instrument:  req.Instrument,
		Timeframe:   req.Timeframe,
		Seq:         req.Seq,
		WindowClose: i%60 == 0,
		Signals: insightsapp.VPVROverloadSignals{
			QueueDepth:          queueDepth,
			QueueCapacity:       100,
			BoundedMapOccupancy: queueDepth,
			BoundedMapLimit:     100,
			ProcessingLatencyMs: latency,
		},
		Snapshot:     out.Snapshot,
		Delta:        out.Delta,
		HasDelta:     true,
		ProcessingMs: latency,
	})
}

func appendEmitSignatures(t *testing.T, emitted *[]string, seq int64, decision insightsapp.VPVROverloadOutput) {
	t.Helper()
	if decision.EmitSnapshot {
		raw, err := insightsapp.MarshalVPVRSnapshotStableBytes(decision.Snapshot)
		if err != nil {
			t.Fatalf("marshal snapshot seq=%d: %v", seq, err)
		}
		*emitted = append(*emitted, fmt.Sprintf("S:%d:%s", seq, sharedhash.HashBytes(raw)))
	}
	if decision.EmitDelta {
		raw, err := insightsapp.MarshalVPVRSnapshotStableBytes(decision.Delta)
		if err != nil {
			t.Fatalf("marshal delta seq=%d: %v", seq, err)
		}
		*emitted = append(*emitted, fmt.Sprintf("D:%d:%s", seq, sharedhash.HashBytes(raw)))
	}
}

func assertWindowCloseInvariant(
	t *testing.T,
	i int,
	builderSnapshot insightsdomain.VolumeProfileSnapshotV1,
	policySnapshot insightsdomain.VolumeProfileSnapshotV1,
	emitSnapshot bool,
) string {
	t.Helper()
	if !emitSnapshot {
		t.Fatalf("window_close must emit snapshot i=%d", i)
	}
	gotRaw, err := insightsapp.MarshalVPVRSnapshotStableBytes(policySnapshot)
	if err != nil {
		t.Fatalf("marshal policy snapshot i=%d: %v", i, err)
	}
	wantRaw, err := insightsapp.MarshalVPVRSnapshotStableBytes(builderSnapshot)
	if err != nil {
		t.Fatalf("marshal builder snapshot i=%d: %v", i, err)
	}
	gotHash := sharedhash.HashBytes(gotRaw)
	wantHash := sharedhash.HashBytes(wantRaw)
	if gotHash != wantHash {
		t.Fatalf("overload changed final builder state i=%d got=%s want=%s", i, gotHash, wantHash)
	}
	return gotHash
}

func commitWindowAndAck(t *testing.T, committer *storage.SnapshotCommitter, rec *commitRecorder, ackSeq int64) {
	t.Helper()
	p := storage.CommitAndAck(context.Background(), committer, aggdomain.SnapshotProduced{
		BookID: aggdomain.BookID{Venue: "binance", Instrument: "BTCUSDT"},
		Seq:    ackSeq,
		Bids:   []aggdomain.Level{{Price: 100, Quantity: 1}},
		Asks:   []aggdomain.Level{{Price: 101, Quantity: 1}},
	}, rec.ackFn(ackSeq))
	if p != nil {
		t.Fatalf("commit+ack failed window=%d: %v", ackSeq, p)
	}
}

func assertOverloadLevelsSeen(t *testing.T, sawL2, sawL3 bool) {
	t.Helper()
	if !sawL2 || !sawL3 {
		t.Fatalf("expected overload levels L2/L3, sawL2=%v sawL3=%v", sawL2, sawL3)
	}
}

func assertAckBoundary(t *testing.T, rec *commitRecorder, windowCloseCount int64) {
	t.Helper()
	for seq := int64(1); seq <= windowCloseCount; seq++ {
		if !rec.ackCommitted[seq] {
			t.Fatalf("missing ack for window seq=%d", seq)
		}
	}
	if got, want := len(rec.order), int(windowCloseCount)*3; got != want {
		t.Fatalf("ack boundary order length=%d want=%d", got, want)
	}
	for idx, seq := 0, int64(1); idx < len(rec.order); idx, seq = idx+3, seq+1 {
		want := []string{fmt.Sprintf("hot:%d", seq), fmt.Sprintf("cold:%d", seq), fmt.Sprintf("ack:%d", seq)}
		got := rec.order[idx : idx+3]
		if !slices.Equal(got, want) {
			t.Fatalf("ack boundary order mismatch seq=%d got=%v want=%v", seq, got, want)
		}
	}
}

func assertRunDeterministic(t *testing.T, a, b integrationRunResult) {
	t.Helper()
	if !slices.Equal(a.emitted, b.emitted) {
		t.Fatalf("emitted sequence not deterministic:\nA=%v\nB=%v", a.emitted, b.emitted)
	}
	if !slices.Equal(a.order, b.order) {
		t.Fatalf("ack order not deterministic:\nA=%v\nB=%v", a.order, b.order)
	}
	if !slices.Equal(a.windowFinal, b.windowFinal) {
		t.Fatalf("window final state not deterministic:\nA=%v\nB=%v", a.windowFinal, b.windowFinal)
	}
}

var _ aggports.HotReadModelStore = commitRecorderHot{}
var _ aggports.ColdReadModelStore = commitRecorderCold{}
