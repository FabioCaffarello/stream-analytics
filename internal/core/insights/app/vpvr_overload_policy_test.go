package app

import (
	"context"
	"slices"
	"testing"

	"github.com/market-raccoon/internal/core/insights/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

func TestNextVPVROverloadLevel_Transitions(t *testing.T) {
	tests := []struct {
		name    string
		prev    VPVROverloadLevel
		signals VPVROverloadSignals
		want    VPVROverloadLevel
	}{
		{
			name: "l0 to l1 by queue",
			prev: VPVROverloadL0,
			signals: VPVROverloadSignals{
				QueueDepth:    60,
				QueueCapacity: 100,
			},
			want: VPVROverloadL1,
		},
		{
			name: "l1 to l2 by latency",
			prev: VPVROverloadL1,
			signals: VPVROverloadSignals{
				ProcessingLatencyMs: 40,
			},
			want: VPVROverloadL2,
		},
		{
			name: "l2 to l3 by occupancy",
			prev: VPVROverloadL2,
			signals: VPVROverloadSignals{
				BoundedMapOccupancy: 96,
				BoundedMapLimit:     100,
			},
			want: VPVROverloadL3,
		},
		{
			name: "l3 to l2 by hysteresis",
			prev: VPVROverloadL3,
			signals: VPVROverloadSignals{
				QueueDepth:          80,
				QueueCapacity:       100,
				BoundedMapOccupancy: 85,
				BoundedMapLimit:     100,
				ProcessingLatencyMs: 50,
			},
			want: VPVROverloadL2,
		},
		{
			name: "l1 to l0 by hysteresis",
			prev: VPVROverloadL1,
			signals: VPVROverloadSignals{
				QueueDepth:          49,
				QueueCapacity:       100,
				BoundedMapOccupancy: 59,
				BoundedMapLimit:     100,
				ProcessingLatencyMs: 14,
			},
			want: VPVROverloadL0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextVPVROverloadLevel(tc.prev, tc.signals); got != tc.want {
				t.Fatalf("level=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestEvaluateVPVROverload_IsPureDeterministic(t *testing.T) {
	in := VPVROverloadInput{
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Timeframe:  "1m",
		Signals: VPVROverloadSignals{
			QueueDepth:    40,
			QueueCapacity: 100,
		},
		PartitionState: VPVROverloadState{
			Level:      VPVROverloadL1,
			EventCount: 10,
		},
		HasDelta: true,
	}

	a := EvaluateVPVROverload(in)
	b := EvaluateVPVROverload(in)

	if a.NextState != b.NextState {
		t.Fatalf("state mismatch: a=%+v b=%+v", a.NextState, b.NextState)
	}
	if a.Level != b.Level {
		t.Fatalf("level mismatch: a=%d b=%d", a.Level, b.Level)
	}
	if a.EmitSnapshot != b.EmitSnapshot {
		t.Fatalf("snapshot emission mismatch: a=%v b=%v", a.EmitSnapshot, b.EmitSnapshot)
	}
	if a.EmitDelta != b.EmitDelta {
		t.Fatalf("delta emission mismatch: a=%v b=%v", a.EmitDelta, b.EmitDelta)
	}
}

func TestEvaluateVPVROverload_CompressesOpenWindowDeterministic(t *testing.T) {
	snapshot := testVPVRSnapshot(8)
	in := VPVROverloadInput{
		WindowClose: false,
		Snapshot:    snapshot,
		PartitionState: VPVROverloadState{
			Level: VPVROverloadL1,
		},
		Signals: VPVROverloadSignals{
			QueueDepth:    95,
			QueueCapacity: 100,
		},
	}

	out := EvaluateVPVROverload(in)
	if !out.Compressed {
		t.Fatal("expected compressed snapshot")
	}
	if got, want := len(out.Snapshot.Buckets), 2; got != want {
		t.Fatalf("bucket count=%d want=%d", got, want)
	}
	if out.CompressRatio != 0.25 {
		t.Fatalf("compress ratio=%f want=0.25", out.CompressRatio)
	}

	out2 := EvaluateVPVROverload(in)
	if len(out2.Snapshot.Buckets) != len(out.Snapshot.Buckets) {
		t.Fatalf("nondeterministic compressed bucket count: first=%d second=%d", len(out.Snapshot.Buckets), len(out2.Snapshot.Buckets))
	}
	if out2.CompressRatio != out.CompressRatio {
		t.Fatalf("nondeterministic compress ratio: first=%f second=%f", out.CompressRatio, out2.CompressRatio)
	}
}

func TestEvaluateVPVROverload_DoesNotCompressWindowClose(t *testing.T) {
	snapshot := testVPVRSnapshot(8)
	in := VPVROverloadInput{
		WindowClose: true,
		Snapshot:    snapshot,
		PartitionState: VPVROverloadState{
			Level: VPVROverloadL3,
		},
		Signals: VPVROverloadSignals{
			QueueDepth:    95,
			QueueCapacity: 100,
		},
	}
	out := EvaluateVPVROverload(in)
	if out.Compressed {
		t.Fatal("window close snapshot must not be compressed")
	}
	if out.CompressRatio != 1.0 {
		t.Fatalf("compress ratio=%f want=1", out.CompressRatio)
	}
	if got, want := len(out.Snapshot.Buckets), len(snapshot.Buckets); got != want {
		t.Fatalf("bucket count=%d want=%d", got, want)
	}
}

func TestEvaluateVPVROverload_DegradeCadenceByCountNoClock(t *testing.T) {
	snapshot := testVPVRSnapshot(4)
	state := VPVROverloadState{Level: VPVROverloadL2}
	emits := make([]bool, 0, 6)
	for i := 0; i < 6; i++ {
		out := EvaluateVPVROverload(VPVROverloadInput{
			Snapshot:       snapshot,
			PartitionState: state,
			Signals: VPVROverloadSignals{
				QueueDepth:    80,
				QueueCapacity: 100,
			},
		})
		emits = append(emits, out.EmitSnapshot)
		state = out.NextState
	}

	want := []bool{false, true, false, true, false, true}
	for i := range want {
		if emits[i] != want[i] {
			t.Fatalf("emit[%d]=%v want=%v sequence=%v", i, emits[i], want[i], emits)
		}
	}
}

func TestEvaluateVPVROverload_CadenceNeverDropsWindowClose(t *testing.T) {
	out := EvaluateVPVROverload(VPVROverloadInput{
		WindowClose: true,
		Snapshot:    testVPVRSnapshot(4),
		PartitionState: VPVROverloadState{
			Level:      VPVROverloadL3,
			EventCount: 1,
		},
		Signals: VPVROverloadSignals{
			QueueDepth:    95,
			QueueCapacity: 100,
		},
	})
	if !out.EmitSnapshot {
		t.Fatal("window close must always emit snapshot")
	}
}

func TestEvaluateVPVROverload_DropDeltaL3ButKeepWindowClose(t *testing.T) {
	open := EvaluateVPVROverload(VPVROverloadInput{
		WindowClose: false,
		HasDelta:    true,
		Snapshot:    testVPVRSnapshot(4),
		PartitionState: VPVROverloadState{
			Level: VPVROverloadL3,
		},
		Signals: VPVROverloadSignals{
			QueueDepth:    95,
			QueueCapacity: 100,
		},
	})
	if open.EmitDelta {
		t.Fatal("expected delta drop in L3 open window")
	}
	if !open.DeltaDropped || open.DropReason != "delta_l3" {
		t.Fatalf("drop info=%v/%q want dropped delta_l3", open.DeltaDropped, open.DropReason)
	}

	closeOut := EvaluateVPVROverload(VPVROverloadInput{
		WindowClose: true,
		HasDelta:    true,
		Snapshot:    testVPVRSnapshot(4),
		PartitionState: VPVROverloadState{
			Level: VPVROverloadL3,
		},
		Signals: VPVROverloadSignals{
			QueueDepth:    95,
			QueueCapacity: 100,
		},
	})
	if !closeOut.EmitDelta {
		t.Fatal("window close must keep delta")
	}
	if closeOut.DeltaDropped {
		t.Fatal("window close delta must not be marked dropped")
	}
}

func TestEvaluateVPVROverload_DropDeltaL2DeterministicEveryOther(t *testing.T) {
	state := VPVROverloadState{Level: VPVROverloadL2}
	got := make([]bool, 0, 6)
	for i := 0; i < 6; i++ {
		out := EvaluateVPVROverload(VPVROverloadInput{
			HasDelta:       true,
			Snapshot:       testVPVRSnapshot(4),
			PartitionState: state,
			Signals: VPVROverloadSignals{
				QueueDepth:    80,
				QueueCapacity: 100,
			},
		})
		got = append(got, out.EmitDelta)
		state = out.NextState
	}
	want := []bool{false, true, false, true, false, true}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("emit_delta[%d]=%v want=%v seq=%v", i, got[i], want[i], got)
		}
	}
}

func testVPVRSnapshot(count int) domain.VolumeProfileSnapshotV1 {
	buckets := make([]domain.VolumeProfileBucketV1, 0, count)
	for i := 0; i < count; i++ {
		low := 100.0 + float64(i)
		buckets = append(buckets, domain.VolumeProfileBucketV1{
			PriceLow:    low,
			PriceHigh:   low + 0.5,
			BuyVolume:   float64(i + 1),
			SellVolume:  float64(i + 2),
			TotalVolume: float64((i + 1) + (i + 2)),
			SeqMin:      int64(i + 1),
			SeqMax:      int64(i + 1),
		})
	}
	return domain.VolumeProfileSnapshotV1{
		Venue:         "BINANCE",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1_710_000_000_000,
		WindowEndTs:   1_710_000_060_000,
		Buckets:       buckets,
		POCPrice:      buckets[len(buckets)-1].PriceLow,
		ValueAreaLow:  buckets[0].PriceLow,
		ValueAreaHigh: buckets[len(buckets)-1].PriceHigh,
	}
}

func TestVPVROverloadPolicy_BurstDeterministic(t *testing.T) {
	run := func() string {
		uc := NewBuildVolumeProfile()
		policy := NewVPVREmitPolicy()
		hashes := make([]string, 0, 4096)
		for i := 1; i <= 2000; i++ {
			side := "buy"
			if i%2 != 0 {
				side = "sell"
			}
			req := BuildVolumeProfileRequest{
				EventType:  "marketdata.trade",
				Venue:      "binance",
				Instrument: "BTC-USDT",
				Timeframe:  "1m",
				TickSize:   0.5,
				Price:      100 + float64(i%64)*0.5,
				Size:       1 + float64(i%7),
				Side:       side,
				TsIngest:   1_710_000_000_000 + int64(i)*1_000,
				Seq:        int64(i),
			}
			res := uc.Execute(context.Background(), req)
			if res.IsFail() {
				t.Fatalf("execute failed i=%d: %v", i, res.Problem())
			}
			out := res.Value()
			if !out.Emitted {
				continue
			}
			windowClose := i%60 == 0
			decision := policy.Apply(VPVROverloadInput{
				Venue:       req.Venue,
				Instrument:  req.Instrument,
				Timeframe:   req.Timeframe,
				Seq:         req.Seq,
				WindowClose: windowClose,
				Signals: VPVROverloadSignals{
					QueueDepth:          (i * 37) % 100,
					QueueCapacity:       100,
					BoundedMapOccupancy: (i * 13) % 100,
					BoundedMapLimit:     100,
					ProcessingLatencyMs: float64((i * 7) % 120),
				},
				Snapshot:     out.Snapshot,
				Delta:        out.Delta,
				HasDelta:     true,
				ProcessingMs: float64((i * 5) % 20),
			})
			if decision.EmitSnapshot {
				raw, err := MarshalVPVRSnapshotStableBytes(decision.Snapshot)
				if err != nil {
					t.Fatalf("marshal snapshot i=%d: %v", i, err)
				}
				hashes = append(hashes, "s:"+sharedhash.HashBytes(raw))
			}
			if decision.EmitDelta {
				raw, err := MarshalVPVRSnapshotStableBytes(decision.Delta)
				if err != nil {
					t.Fatalf("marshal delta i=%d: %v", i, err)
				}
				hashes = append(hashes, "d:"+sharedhash.HashBytes(raw))
			}
		}
		return sharedhash.HashFields(hashes...)
	}

	h1 := run()
	h2 := run()
	if h1 != h2 {
		t.Fatalf("burst deterministic hash mismatch: %s vs %s", h1, h2)
	}
}

func TestVPVROverloadPolicy_SameWindowFinalStateUnchanged(t *testing.T) {
	uc := NewBuildVolumeProfile()
	policy := NewVPVREmitPolicy()

	for i := 1; i <= 240; i++ {
		side := "buy"
		if i%2 != 0 {
			side = "sell"
		}
		req := BuildVolumeProfileRequest{
			EventType:  "marketdata.trade",
			Venue:      "binance",
			Instrument: "BTC-USDT",
			Timeframe:  "1m",
			TickSize:   0.5,
			Price:      100 + float64(i%32)*0.5,
			Size:       1 + float64(i%5),
			Side:       side,
			TsIngest:   1_710_000_000_000 + int64(i)*1_000,
			Seq:        int64(i),
		}
		res := uc.Execute(context.Background(), req)
		if res.IsFail() {
			t.Fatalf("execute failed i=%d: %v", i, res.Problem())
		}
		out := res.Value()
		if !out.Emitted {
			continue
		}
		windowClose := i%60 == 0
		decision := policy.Apply(VPVROverloadInput{
			Venue:       req.Venue,
			Instrument:  req.Instrument,
			Timeframe:   req.Timeframe,
			Seq:         req.Seq,
			WindowClose: windowClose,
			Signals: VPVROverloadSignals{
				QueueDepth:          95,
				QueueCapacity:       100,
				BoundedMapOccupancy: 95,
				BoundedMapLimit:     100,
				ProcessingLatencyMs: 100,
			},
			Snapshot:     out.Snapshot,
			Delta:        out.Delta,
			HasDelta:     true,
			ProcessingMs: 10,
		})
		if !windowClose {
			continue
		}
		if !decision.EmitSnapshot {
			t.Fatalf("window close i=%d snapshot must emit", i)
		}
		gotRaw, err := MarshalVPVRSnapshotStableBytes(decision.Snapshot)
		if err != nil {
			t.Fatalf("marshal policy snapshot i=%d: %v", i, err)
		}
		wantRaw, err := MarshalVPVRSnapshotStableBytes(out.Snapshot)
		if err != nil {
			t.Fatalf("marshal builder snapshot i=%d: %v", i, err)
		}
		if sharedhash.HashBytes(gotRaw) != sharedhash.HashBytes(wantRaw) {
			t.Fatalf("window close final-state changed at i=%d", i)
		}
	}
}

func TestVPVROverloadPolicy_EmittedSequenceEquivalentToLegacy(t *testing.T) {
	snapshot := testVPVRSnapshot(8)
	stateNew := VPVROverloadState{}
	stateLegacy := VPVROverloadState{}
	newSeq := make([]string, 0, 512)
	legacySeq := make([]string, 0, 512)

	for i := 1; i <= 240; i++ {
		queueDepth := 82
		latency := 50.0
		if i%2 == 0 {
			queueDepth = 95
			latency = 100
		}
		in := VPVROverloadInput{
			WindowClose: i%60 == 0,
			Signals: VPVROverloadSignals{
				QueueDepth:          queueDepth,
				QueueCapacity:       100,
				BoundedMapOccupancy: queueDepth,
				BoundedMapLimit:     100,
				ProcessingLatencyMs: latency,
			},
			Snapshot:       snapshot,
			Delta:          snapshot,
			HasDelta:       true,
			PartitionState: stateNew,
		}
		newOut := EvaluateVPVROverload(in)
		legacyOut := legacyEvaluateVPVROverload(in, stateLegacy)
		stateNew = newOut.NextState
		stateLegacy = legacyOut.NextState

		if newOut.EmitSnapshot {
			newSeq = append(newSeq, "S")
		}
		if newOut.EmitDelta {
			newSeq = append(newSeq, "D")
		}
		if legacyOut.EmitSnapshot {
			legacySeq = append(legacySeq, "S")
		}
		if legacyOut.EmitDelta {
			legacySeq = append(legacySeq, "D")
		}
	}

	if !slices.Equal(newSeq, legacySeq) {
		t.Fatalf("emitted sequence mismatch\nnew=%v\nlegacy=%v", newSeq, legacySeq)
	}
}

func legacyEvaluateVPVROverload(input VPVROverloadInput, state VPVROverloadState) VPVROverloadOutput {
	next := state
	next.Level = legacyNextLevel(state.Level, input.Signals)
	next.EventCount++

	snapshot := input.Snapshot
	compressed := false
	compressRatio := 1.0
	if !input.WindowClose {
		snapshot, compressed, compressRatio = compressSnapshotByLevel(snapshot, next.Level)
	}
	emitSnapshot := legacyShouldEmitSnapshot(next.EventCount, next.Level, input.WindowClose)
	emitDelta, reason := legacyShouldEmitDelta(next.EventCount, next.Level, input.WindowClose, input.HasDelta)

	return VPVROverloadOutput{
		NextState:      next,
		Level:          next.Level,
		Snapshot:       snapshot,
		EmitSnapshot:   emitSnapshot,
		Delta:          input.Delta,
		EmitDelta:      emitDelta,
		Compressed:     compressed,
		CompressRatio:  compressRatio,
		CadenceDropped: !emitSnapshot,
		DeltaDropped:   input.HasDelta && !emitDelta,
		DropReason:     reason,
	}
}

func legacyNextLevel(prev VPVROverloadLevel, signals VPVROverloadSignals) VPVROverloadLevel {
	queueRatio := legacyRatio(signals.QueueDepth, signals.QueueCapacity)
	mapRatio := legacyRatio(signals.BoundedMapOccupancy, signals.BoundedMapLimit)
	latencyMs := signals.ProcessingLatencyMs

	severity := legacyClassify(queueRatio, mapRatio, latencyMs)
	switch prev {
	case VPVROverloadL0:
		return severity
	case VPVROverloadL1:
		if severity >= VPVROverloadL2 {
			return severity
		}
		if queueRatio < 0.50 && mapRatio < 0.60 && latencyMs < 15 {
			return VPVROverloadL0
		}
		return VPVROverloadL1
	case VPVROverloadL2:
		if severity == VPVROverloadL3 {
			return VPVROverloadL3
		}
		if queueRatio < 0.70 && mapRatio < 0.80 && latencyMs < 30 {
			return VPVROverloadL1
		}
		return VPVROverloadL2
	default:
		if queueRatio < 0.85 && mapRatio < 0.90 && latencyMs < 60 {
			return VPVROverloadL2
		}
		return VPVROverloadL3
	}
}

func legacyClassify(queueRatio, mapRatio, latencyMs float64) VPVROverloadLevel {
	if queueRatio >= 0.92 || mapRatio >= 0.95 || latencyMs >= 80 {
		return VPVROverloadL3
	}
	if queueRatio >= 0.80 || mapRatio >= 0.85 || latencyMs >= 40 {
		return VPVROverloadL2
	}
	if queueRatio >= 0.60 || mapRatio >= 0.70 || latencyMs >= 20 {
		return VPVROverloadL1
	}
	return VPVROverloadL0
}

func legacyRatio(current, capacity int) float64 {
	if capacity <= 0 || current <= 0 {
		return 0
	}
	return float64(current) / float64(capacity)
}

func legacyShouldEmitSnapshot(eventCount uint64, level VPVROverloadLevel, windowClose bool) bool {
	if windowClose {
		return true
	}
	stride := 1
	if level == VPVROverloadL2 {
		stride = 2
	}
	if level == VPVROverloadL3 {
		stride = 4
	}
	if stride <= 1 {
		return true
	}
	return eventCount%uint64(stride) == 0
}

func legacyShouldEmitDelta(eventCount uint64, level VPVROverloadLevel, windowClose bool, hasDelta bool) (bool, string) {
	if !hasDelta {
		return false, ""
	}
	if windowClose {
		return true, ""
	}
	switch level {
	case VPVROverloadL3:
		return false, "delta_l3"
	case VPVROverloadL2:
		if eventCount%2 == 1 {
			return false, "delta_l2"
		}
		return true, ""
	default:
		return true, ""
	}
}
