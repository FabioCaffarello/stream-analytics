package app

import "testing"

func TestSessionEmitPolicy_NormalCadence(t *testing.T) {
	p := NewSessionEmitPolicy(nil)
	emits := 0
	for i := 0; i < 10; i++ {
		if p.ShouldEmit("test|key", 5, false, SessionEmitSignals{}) {
			emits++
		}
	}
	if emits != 2 { // event 5 and 10
		t.Errorf("expected 2 emissions, got %d", emits)
	}
}

func TestSessionEmitPolicy_SessionCloseAlwaysEmits(t *testing.T) {
	p := NewSessionEmitPolicy(nil)
	// Even on event 1 (not cadence-aligned), session close emits.
	if !p.ShouldEmit("test|key", 5, true, SessionEmitSignals{}) {
		t.Fatal("session close should always emit")
	}
}

func TestSessionEmitPolicy_OverloadReducesCadence(t *testing.T) {
	p := NewSessionEmitPolicy(nil)
	// 80% occupancy → L2, cadence = 5*4 = 20
	highLoad := SessionEmitSignals{QueueDepth: 800, QueueCapacity: 1000, ProcessingLatencyMs: 5}
	emits := 0
	for i := 0; i < 20; i++ {
		if p.ShouldEmit("test|key", 5, false, highLoad) {
			emits++
		}
	}
	// At L2 (75%+ occupancy), cadence = 20 → 1 emission in 20 events.
	if emits != 1 {
		t.Errorf("expected 1 emission under L2 load, got %d", emits)
	}
}

func TestSessionEmitPolicy_CriticalLoadSuppresses(t *testing.T) {
	p := NewSessionEmitPolicy(nil)
	criticalLoad := SessionEmitSignals{QueueDepth: 950, QueueCapacity: 1000, ProcessingLatencyMs: 100}
	emits := 0
	for i := 0; i < 100; i++ {
		if p.ShouldEmit("test|key", 5, false, criticalLoad) {
			emits++
		}
	}
	if emits != 0 {
		t.Errorf("expected 0 emissions under L3 load, got %d", emits)
	}
}

func TestEffectiveCadence(t *testing.T) {
	tests := []struct {
		base  int
		level SessionEmitLevel
		want  int
	}{
		{5, SessionEmitL0, 5},
		{5, SessionEmitL1, 10},
		{5, SessionEmitL2, 20},
		{5, SessionEmitL3, 5000000},
		{0, SessionEmitL0, 5}, // default base
	}
	for _, tt := range tests {
		got := effectiveCadence(tt.base, tt.level)
		if got != tt.want {
			t.Errorf("effectiveCadence(%d, L%d): got %d, want %d", tt.base, tt.level, got, tt.want)
		}
	}
}
