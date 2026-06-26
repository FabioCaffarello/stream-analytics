package app

import "sync"

// SessionEmitLevel represents overload severity for session profiles.
type SessionEmitLevel int

const (
	SessionEmitL0 SessionEmitLevel = 0 // normal: emit every EmitCadence
	SessionEmitL1 SessionEmitLevel = 1 // moderate: 2x cadence
	SessionEmitL2 SessionEmitLevel = 2 // elevated: 4x cadence
	SessionEmitL3 SessionEmitLevel = 3 // critical: session-close only
)

// SessionEmitSignals carries runtime load signals for session profile emission.
type SessionEmitSignals struct {
	QueueDepth          int
	QueueCapacity       int
	ProcessingLatencyMs float64
}

// SessionEmitDecideFunc resolves overload level from previous level and signals.
type SessionEmitDecideFunc func(prev SessionEmitLevel, signals SessionEmitSignals) SessionEmitLevel

// SessionEmitPolicy manages per-partition emission cadence for session profiles.
type SessionEmitPolicy struct {
	mu     sync.Mutex
	decide SessionEmitDecideFunc
	states map[string]*sessionEmitState
}

type sessionEmitState struct {
	level      SessionEmitLevel
	eventCount uint64
}

func NewSessionEmitPolicy(decide SessionEmitDecideFunc) *SessionEmitPolicy {
	if decide == nil {
		decide = defaultSessionDecide
	}
	return &SessionEmitPolicy{
		decide: decide,
		states: make(map[string]*sessionEmitState),
	}
}

// ShouldEmit returns true if emission is allowed for the given partition.
// baseCadence is the configured emit cadence (e.g. 5).
func (p *SessionEmitPolicy) ShouldEmit(partitionKey string, baseCadence int, isSessionClose bool, signals SessionEmitSignals) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	st := p.states[partitionKey]
	if st == nil {
		st = &sessionEmitState{}
		p.states[partitionKey] = st
	}
	st.eventCount++
	st.level = p.decide(st.level, signals)

	// Session close always emits.
	if isSessionClose {
		return true
	}

	cadence := uint64(effectiveCadence(baseCadence, st.level)) //nolint:gosec // cadence is always small positive
	return st.eventCount%cadence == 0
}

func effectiveCadence(base int, level SessionEmitLevel) int {
	if base <= 0 {
		base = 5
	}
	switch level {
	case SessionEmitL1:
		return base * 2
	case SessionEmitL2:
		return base * 4
	case SessionEmitL3:
		return base * 1000000 // effectively never (close-only)
	default:
		return base
	}
}

func defaultSessionDecide(prev SessionEmitLevel, signals SessionEmitSignals) SessionEmitLevel {
	if signals.QueueCapacity <= 0 {
		return SessionEmitL0
	}
	occupancy := float64(signals.QueueDepth) / float64(signals.QueueCapacity)
	switch {
	case occupancy > 0.90 || signals.ProcessingLatencyMs > 50:
		return SessionEmitL3
	case occupancy > 0.75 || signals.ProcessingLatencyMs > 20:
		return SessionEmitL2
	case occupancy > 0.50 || signals.ProcessingLatencyMs > 10:
		return SessionEmitL1
	default:
		return SessionEmitL0
	}
}
