package app

import (
	"time"

	"github.com/market-raccoon/internal/core/evidence/domain"
	"github.com/market-raccoon/internal/shared/metrics"
)

// EvidenceEmitter publishes canonical evidence events.
type EvidenceEmitter interface {
	Emit(ev domain.EvidenceEvent)
}

// EngineConfig configures the EvidenceEngine.
type EngineConfig struct {
	MaxStreamsPerRule int
	MaxStreamsGlobal  int
	StreamTTLMillis   int64
	BufferCapPerKind  int // kept for backward-compatible config wiring
	DecayHalfLife     time.Duration
}

// DefaultEngineConfig returns production defaults.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MaxStreamsPerRule: 256,
		MaxStreamsGlobal:  1024,
		StreamTTLMillis:   10 * 60 * 1000,
		BufferCapPerKind:  1000,
		DecayHalfLife:     time.Minute,
	}
}

// EngineStats reports current engine state.
type EngineStats struct {
	TotalStreams int
	TotalEmitted int64
	TotalEvicted int64
	RuleStreams  map[string]int
}

// EvidenceEngine orchestrates all evidence rules with deterministic bounded state.
type EvidenceEngine struct {
	cfg          EngineConfig
	rules        []domain.EvidenceRule
	store        *EvidenceStateStore
	emitter      EvidenceEmitter
	totalEmitted int64
	totalEvicted int64
}

// NewEvidenceEngine creates an engine with the given rules.
func NewEvidenceEngine(cfg EngineConfig, rules ...domain.EvidenceRule) *EvidenceEngine {
	return NewEvidenceEngineWithEmitter(cfg, nil, rules...)
}

// NewEvidenceEngineWithEmitter creates an engine with an optional event emitter.
func NewEvidenceEngineWithEmitter(cfg EngineConfig, emitter EvidenceEmitter, rules ...domain.EvidenceRule) *EvidenceEngine {
	if cfg.MaxStreamsGlobal <= 0 {
		cfg.MaxStreamsGlobal = 1024
	}
	if cfg.StreamTTLMillis <= 0 {
		cfg.StreamTTLMillis = 10 * 60 * 1000
	}
	if cfg.MaxStreamsPerRule <= 0 {
		cfg.MaxStreamsPerRule = 256
	}
	return &EvidenceEngine{
		cfg:     cfg,
		rules:   rules,
		store:   NewEvidenceStateStore(EvidenceStateStoreConfig{MaxEntries: cfg.MaxStreamsGlobal, TTLMillis: cfg.StreamTTLMillis}),
		emitter: emitter,
	}
}

// OnEvent dispatches one canonical rule event to all rules.
func (e *EvidenceEngine) OnEvent(event domain.RuleEvent) []domain.EvidenceEvent {
	if e == nil {
		return nil
	}
	obs := e.store.Observe(event.StreamID, event.Seq, event.TsServer)
	for _, eviction := range obs.Evictions {
		for i := range e.rules {
			e.rules[i].EvictStream(eviction.StreamID)
		}
		metrics.IncEvidenceStateEvicted(eviction.Reason)
		e.totalEvicted++
	}
	metrics.SetEvidenceStateEntries(e.store.Len())
	if !obs.Accepted {
		metrics.IncEvidenceDropped(obs.Reason)
		return nil
	}

	if obs.PrevTs > 0 && event.TsServer > obs.PrevTs {
		metrics.ObserveEvidenceEvalLatency(float64(event.TsServer-obs.PrevTs) / 1000.0)
	} else {
		metrics.ObserveEvidenceEvalLatency(0)
	}

	out := make([]domain.EvidenceEvent, 0, len(e.rules))
	for i := range e.rules {
		ruleEvents := e.rules[i].OnEvent(event)
		for j := range ruleEvents {
			ev := ruleEvents[j]
			ev.StreamID = event.StreamID
			if ev.Seq <= 0 {
				ev.Seq = event.Seq
			}
			if ev.InputWatermark.SeqStart <= 0 {
				ev.InputWatermark.SeqStart = event.Seq
			}
			if ev.InputWatermark.SeqEnd <= 0 {
				ev.InputWatermark.SeqEnd = event.Seq
			}
			ev.Features = domain.SortedFeatures(ev.Features)
			if p := ev.Validate(); p != nil {
				metrics.IncEvidenceDropped("invalid_evidence")
				continue
			}
			out = append(out, ev)
			metrics.IncEvidenceEmitted(string(ev.Type), string(ev.Severity), ev.Venue)
			if e.emitter != nil {
				e.emitter.Emit(ev)
			}
		}
	}
	e.totalEmitted += int64(len(out))
	return out
}

// Stats returns current engine statistics.
func (e *EvidenceEngine) Stats() EngineStats {
	ruleStreams := make(map[string]int, len(e.rules))
	for i := range e.rules {
		ruleStreams[e.rules[i].Name()] = e.rules[i].StreamCount()
	}
	return EngineStats{
		TotalStreams: e.store.Len(),
		TotalEmitted: e.totalEmitted,
		TotalEvicted: e.totalEvicted,
		RuleStreams:  ruleStreams,
	}
}
