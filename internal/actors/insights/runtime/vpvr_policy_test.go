package insightsruntime

import (
	"context"
	"slices"
	"testing"

	insightsapp "github.com/FabioCaffarello/stream-analytics/internal/core/insights/app"
	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
	sharedhash "github.com/FabioCaffarello/stream-analytics/internal/shared/hash"
)

func TestNextVPVROverloadLevel_Transitions(t *testing.T) {
	decide := DefaultDecideFunc()

	tests := []struct {
		name    string
		prev    insightsapp.VPVROverloadLevel
		signals insightsapp.VPVROverloadSignals
		want    insightsapp.VPVROverloadLevel
	}{
		{
			name: "l0 to l1 by queue",
			prev: insightsapp.VPVROverloadL0,
			signals: insightsapp.VPVROverloadSignals{
				QueueDepth:    60,
				QueueCapacity: 100,
			},
			want: insightsapp.VPVROverloadL1,
		},
		{
			name: "l1 to l2 by latency",
			prev: insightsapp.VPVROverloadL1,
			signals: insightsapp.VPVROverloadSignals{
				ProcessingLatencyMs: 40,
			},
			want: insightsapp.VPVROverloadL2,
		},
		{
			name: "l2 to l3 by occupancy",
			prev: insightsapp.VPVROverloadL2,
			signals: insightsapp.VPVROverloadSignals{
				BoundedMapOccupancy: 96,
				BoundedMapLimit:     100,
			},
			want: insightsapp.VPVROverloadL3,
		},
		{
			name: "l3 to l2 by hysteresis",
			prev: insightsapp.VPVROverloadL3,
			signals: insightsapp.VPVROverloadSignals{
				QueueDepth:          80,
				QueueCapacity:       100,
				BoundedMapOccupancy: 85,
				BoundedMapLimit:     100,
				ProcessingLatencyMs: 50,
			},
			want: insightsapp.VPVROverloadL2,
		},
		{
			name: "l1 to l0 by hysteresis",
			prev: insightsapp.VPVROverloadL1,
			signals: insightsapp.VPVROverloadSignals{
				QueueDepth:          49,
				QueueCapacity:       100,
				BoundedMapOccupancy: 59,
				BoundedMapLimit:     100,
				ProcessingLatencyMs: 14,
			},
			want: insightsapp.VPVROverloadL0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, _, _ := decide(tc.prev, tc.signals)
			if got != tc.want {
				t.Fatalf("level=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestVPVROverloadPolicy_BurstDeterministic(t *testing.T) {
	run := func() string {
		uc := insightsapp.NewBuildVolumeProfile()
		policy := insightsapp.NewVPVREmitPolicy(DefaultDecideFunc())
		hashes := make([]string, 0, 4096)
		for i := 1; i <= 2000; i++ {
			side := "buy"
			if i%2 != 0 {
				side = "sell"
			}
			req := insightsapp.BuildVolumeProfileRequest{
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
			decision := policy.Apply(insightsapp.VPVROverloadInput{
				Venue:       req.Venue,
				Instrument:  req.Instrument,
				Timeframe:   req.Timeframe,
				Seq:         req.Seq,
				WindowClose: windowClose,
				Signals: insightsapp.VPVROverloadSignals{
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
				raw, err := insightsapp.MarshalVPVRSnapshotStableBytes(decision.Snapshot)
				if err != nil {
					t.Fatalf("marshal snapshot i=%d: %v", i, err)
				}
				hashes = append(hashes, "s:"+sharedhash.HashBytes(raw))
			}
			if decision.EmitDelta {
				raw, err := insightsapp.MarshalVPVRSnapshotStableBytes(decision.Delta)
				if err != nil {
					t.Fatalf("marshal delta i=%d: %v", i, err)
				}
				hashes = append(hashes, "d:"+sharedhash.HashBytes(raw))
			}
		}
		return sharedhash.HashFieldsFast(hashes...)
	}

	h1 := run()
	h2 := run()
	if h1 != h2 {
		t.Fatalf("burst deterministic hash mismatch: %s vs %s", h1, h2)
	}
}

func TestVPVROverloadPolicy_SameWindowFinalStateUnchanged(t *testing.T) {
	uc := insightsapp.NewBuildVolumeProfile()
	policy := insightsapp.NewVPVREmitPolicy(DefaultDecideFunc())

	for i := 1; i <= 240; i++ {
		side := "buy"
		if i%2 != 0 {
			side = "sell"
		}
		req := insightsapp.BuildVolumeProfileRequest{
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
		decision := policy.Apply(insightsapp.VPVROverloadInput{
			Venue:       req.Venue,
			Instrument:  req.Instrument,
			Timeframe:   req.Timeframe,
			Seq:         req.Seq,
			WindowClose: windowClose,
			Signals: insightsapp.VPVROverloadSignals{
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
		gotRaw, err := insightsapp.MarshalVPVRSnapshotStableBytes(decision.Snapshot)
		if err != nil {
			t.Fatalf("marshal policy snapshot i=%d: %v", i, err)
		}
		wantRaw, err := insightsapp.MarshalVPVRSnapshotStableBytes(out.Snapshot)
		if err != nil {
			t.Fatalf("marshal builder snapshot i=%d: %v", i, err)
		}
		if sharedhash.HashBytes(gotRaw) != sharedhash.HashBytes(wantRaw) {
			t.Fatalf("window close final-state changed at i=%d", i)
		}
	}
}

func TestVPVROverloadPolicy_EmittedSequenceEquivalentToLegacy(t *testing.T) {
	decide := DefaultDecideFunc()
	snapshot := testVPVRSnapshot(8)
	stateNew := insightsapp.VPVROverloadState{}
	stateLegacy := insightsapp.VPVROverloadState{}
	newSeq := make([]string, 0, 512)
	legacySeq := make([]string, 0, 512)

	for i := 1; i <= 240; i++ {
		queueDepth := 82
		latency := 50.0
		if i%2 == 0 {
			queueDepth = 95
			latency = 100
		}
		signals := insightsapp.VPVROverloadSignals{
			QueueDepth:          queueDepth,
			QueueCapacity:       100,
			BoundedMapOccupancy: queueDepth,
			BoundedMapLimit:     100,
			ProcessingLatencyMs: latency,
		}
		in := insightsapp.VPVROverloadInput{
			WindowClose:    i%60 == 0,
			Signals:        signals,
			Snapshot:       snapshot,
			Delta:          snapshot,
			HasDelta:       true,
			PartitionState: stateNew,
		}
		nextLevel, compress, stride, drop := decide(stateNew.Level, signals)
		newOut := insightsapp.EvaluateVPVROverload(in, nextLevel, compress, stride, drop)

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

// --- legacy helpers for equivalence testing ---

func legacyEvaluateVPVROverload(input insightsapp.VPVROverloadInput, state insightsapp.VPVROverloadState) insightsapp.VPVROverloadOutput {
	next := state
	next.Level = legacyNextLevel(state.Level, input.Signals)
	next.EventCount++
	compress := next.Level >= insightsapp.VPVROverloadL1
	stride := 1
	if next.Level == insightsapp.VPVROverloadL2 {
		stride = 2
	}
	if next.Level == insightsapp.VPVROverloadL3 {
		stride = 4
	}
	drop := next.Level == insightsapp.VPVROverloadL3
	return insightsapp.EvaluateVPVROverload(input, next.Level, compress, stride, drop)
}

func legacyNextLevel(prev insightsapp.VPVROverloadLevel, signals insightsapp.VPVROverloadSignals) insightsapp.VPVROverloadLevel {
	queueRatio := legacyRatio(signals.QueueDepth, signals.QueueCapacity)
	mapRatio := legacyRatio(signals.BoundedMapOccupancy, signals.BoundedMapLimit)
	latencyMs := signals.ProcessingLatencyMs

	severity := legacyClassify(queueRatio, mapRatio, latencyMs)
	switch prev {
	case insightsapp.VPVROverloadL0:
		return severity
	case insightsapp.VPVROverloadL1:
		if severity >= insightsapp.VPVROverloadL2 {
			return severity
		}
		if queueRatio < 0.50 && mapRatio < 0.60 && latencyMs < 15 {
			return insightsapp.VPVROverloadL0
		}
		return insightsapp.VPVROverloadL1
	case insightsapp.VPVROverloadL2:
		if severity == insightsapp.VPVROverloadL3 {
			return insightsapp.VPVROverloadL3
		}
		if queueRatio < 0.70 && mapRatio < 0.80 && latencyMs < 30 {
			return insightsapp.VPVROverloadL1
		}
		return insightsapp.VPVROverloadL2
	default:
		if queueRatio < 0.85 && mapRatio < 0.90 && latencyMs < 60 {
			return insightsapp.VPVROverloadL2
		}
		return insightsapp.VPVROverloadL3
	}
}

func legacyClassify(queueRatio, mapRatio, latencyMs float64) insightsapp.VPVROverloadLevel {
	if queueRatio >= 0.92 || mapRatio >= 0.95 || latencyMs >= 80 {
		return insightsapp.VPVROverloadL3
	}
	if queueRatio >= 0.80 || mapRatio >= 0.85 || latencyMs >= 40 {
		return insightsapp.VPVROverloadL2
	}
	if queueRatio >= 0.60 || mapRatio >= 0.70 || latencyMs >= 20 {
		return insightsapp.VPVROverloadL1
	}
	return insightsapp.VPVROverloadL0
}

func legacyRatio(current, capacity int) float64 {
	if capacity <= 0 || current <= 0 {
		return 0
	}
	return float64(current) / float64(capacity)
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
