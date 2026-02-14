package policykit

import "testing"

func TestThresholdEngineTransitionsDeterministic(t *testing.T) {
	engine := NewThresholdEngine(DefaultThresholdConfig())

	tests := []struct {
		name    string
		prev    Level
		signals Signals
		want    Level
	}{
		{
			name: "l0 to l1",
			prev: L0,
			signals: Signals{
				QueueDepth:    60,
				QueueCapacity: 100,
			},
			want: L1,
		},
		{
			name: "l1 to l2",
			prev: L1,
			signals: Signals{
				ProcessingLatencyMs: 40,
			},
			want: L2,
		},
		{
			name: "l2 to l3",
			prev: L2,
			signals: Signals{
				Occupancy: 95,
				Limit:     100,
			},
			want: L3,
		},
		{
			name: "l3 to l2 recovery",
			prev: L3,
			signals: Signals{
				QueueDepth:          84,
				QueueCapacity:       100,
				Backlog:             84,
				BacklogCap:          100,
				Occupancy:           89,
				Limit:               100,
				ProcessingLatencyMs: 59,
			},
			want: L2,
		},
		{
			name: "l1 to l0 recovery",
			prev: L1,
			signals: Signals{
				QueueDepth:          49,
				QueueCapacity:       100,
				Backlog:             49,
				BacklogCap:          100,
				Occupancy:           59,
				Limit:               100,
				ProcessingLatencyMs: 14,
			},
			want: L0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := engine.Decide(tc.prev, tc.signals)
			b := engine.Decide(tc.prev, tc.signals)
			if a.Level != tc.want {
				t.Fatalf("level=%d want=%d", a.Level, tc.want)
			}
			if b.Level != tc.want {
				t.Fatalf("second level=%d want=%d", b.Level, tc.want)
			}
			if len(a.Actions) != len(b.Actions) {
				t.Fatalf("actions length mismatch: a=%d b=%d", len(a.Actions), len(b.Actions))
			}
		})
	}
}

func TestThresholdEngineActionsByLevel(t *testing.T) {
	engine := NewThresholdEngine(DefaultThresholdConfig())
	cases := []struct {
		prev              Level
		signals           Signals
		wantDropDelta     bool
		wantCompress      bool
		wantDegradeStride int
	}{
		{prev: L0, signals: Signals{}, wantDegradeStride: 1},
		{prev: L0, signals: Signals{QueueDepth: 60, QueueCapacity: 100}, wantCompress: true, wantDegradeStride: 1},
		{prev: L0, signals: Signals{QueueDepth: 80, QueueCapacity: 100}, wantCompress: true, wantDegradeStride: 2},
		{prev: L0, signals: Signals{QueueDepth: 95, QueueCapacity: 100}, wantDropDelta: true, wantCompress: true, wantDegradeStride: 4},
	}

	for _, tc := range cases {
		decision := engine.Decide(tc.prev, tc.signals)
		if got := decision.HasAction(ActionDropDelta); got != tc.wantDropDelta {
			t.Fatalf("drop=%v want=%v", got, tc.wantDropDelta)
		}
		if got := decision.HasAction(ActionCompressSnapshot); got != tc.wantCompress {
			t.Fatalf("compress=%v want=%v", got, tc.wantCompress)
		}
		if got := decision.DegradeStride(); got != tc.wantDegradeStride {
			t.Fatalf("stride=%d want=%d", got, tc.wantDegradeStride)
		}
	}
}
