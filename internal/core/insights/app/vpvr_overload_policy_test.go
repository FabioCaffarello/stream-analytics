package app

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
)

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

	// L1 with moderate signals: no compress, stride 1, no drop.
	a := EvaluateVPVROverload(in, VPVROverloadL1, false, 1, false)
	b := EvaluateVPVROverload(in, VPVROverloadL1, false, 1, false)

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

	// L3 with compress enabled.
	out := EvaluateVPVROverload(in, VPVROverloadL3, true, 4, true)
	if !out.Compressed {
		t.Fatal("expected compressed snapshot")
	}
	if got, want := len(out.Snapshot.Buckets), 2; got != want {
		t.Fatalf("bucket count=%d want=%d", got, want)
	}
	if out.CompressRatio != 0.25 {
		t.Fatalf("compress ratio=%f want=0.25", out.CompressRatio)
	}

	out2 := EvaluateVPVROverload(in, VPVROverloadL3, true, 4, true)
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
	out := EvaluateVPVROverload(in, VPVROverloadL3, true, 4, true)
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
		}, VPVROverloadL2, true, 2, false)
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
	}, VPVROverloadL3, true, 4, true)
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
	}, VPVROverloadL3, true, 4, true)
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
	}, VPVROverloadL3, true, 4, true)
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
		}, VPVROverloadL2, true, 2, false)
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
