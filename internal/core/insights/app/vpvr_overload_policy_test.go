package app

import "testing"

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
			QueueDepth:    80,
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
	if !a.EmitSnapshot || !b.EmitSnapshot {
		t.Fatal("expected snapshot emission")
	}
	if !a.EmitDelta || !b.EmitDelta {
		t.Fatal("expected delta emission")
	}
}
