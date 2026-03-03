package app

import (
	"time"

	"github.com/market-raccoon/internal/core/evidence/domain"
	"github.com/market-raccoon/internal/shared/metrics"
)

// EngineConfig configures the EvidenceEngine.
type EngineConfig struct {
	MaxStreamsPerRule int
	MaxStreamsGlobal  int
	StreamTTL         time.Duration
	SweepInterval     time.Duration
	Now               func() time.Time // for deterministic tests
}

// DefaultEngineConfig returns production defaults.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MaxStreamsPerRule: 256,
		MaxStreamsGlobal:  1024,
		StreamTTL:         10 * time.Minute,
		SweepInterval:     1 * time.Minute,
		Now:               time.Now,
	}
}

// EngineStats reports current engine state.
type EngineStats struct {
	TotalStreams int
	TotalEmitted int64
	TotalEvicted int64
	RuleStreams  map[string]int
}

// EvidenceEngine orchestrates all evidence rules, enforces global bounds,
// and collects metrics.
type EvidenceEngine struct {
	cfg            EngineConfig
	rules          []domain.EvidenceRule
	streamLastSeen map[string]time.Time
	lastSweep      time.Time
	totalEmitted   int64
	totalEvicted   int64
}

// NewEvidenceEngine creates an engine with the given rules.
func NewEvidenceEngine(cfg EngineConfig, rules ...domain.EvidenceRule) *EvidenceEngine {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &EvidenceEngine{
		cfg:            cfg,
		rules:          rules,
		streamLastSeen: make(map[string]time.Time),
		lastSweep:      cfg.Now(),
	}
}

// OnEvent dispatches a rule event to all rules and returns collected evidence.
func (e *EvidenceEngine) OnEvent(event domain.RuleEvent) []domain.EvidenceEvent {
	metrics.IncEvidenceEngineEvents()

	key := event.StreamKey()
	now := e.cfg.Now()
	e.streamLastSeen[key] = now

	// Periodic sweep
	if now.Sub(e.lastSweep) >= e.cfg.SweepInterval {
		e.Sweep()
	}

	// Global cap enforcement
	if len(e.streamLastSeen) > e.cfg.MaxStreamsGlobal {
		e.evictOldestGlobal()
	}

	var result []domain.EvidenceEvent
	for _, rule := range e.rules {
		events := rule.OnEvent(event)
		for i := range events {
			metrics.IncEvidenceEmitted(string(events[i].Kind), string(events[i].Severity))
		}
		result = append(result, events...)
	}

	e.totalEmitted += int64(len(result))

	// Update state metrics
	totalStreams := 0
	for _, rule := range e.rules {
		count := rule.StreamCount()
		metrics.SetEvidenceStateEntries(rule.Name(), count)
		totalStreams += count
	}
	metrics.SetEvidenceStateEntriesTotal(totalStreams)

	return result
}

// Sweep evicts streams that haven't been seen within StreamTTL.
// Returns the number of evicted global entries.
func (e *EvidenceEngine) Sweep() int {
	now := e.cfg.Now()
	e.lastSweep = now
	evicted := 0

	for key, lastSeen := range e.streamLastSeen {
		if now.Sub(lastSeen) > e.cfg.StreamTTL {
			delete(e.streamLastSeen, key)
			for _, rule := range e.rules {
				rule.EvictStream(key)
				metrics.IncEvidenceStateEvicted(rule.Name())
			}
			evicted++
		}
	}

	e.totalEvicted += int64(evicted)
	return evicted
}

// Stats returns current engine statistics.
func (e *EvidenceEngine) Stats() EngineStats {
	ruleStreams := make(map[string]int, len(e.rules))
	for _, rule := range e.rules {
		ruleStreams[rule.Name()] = rule.StreamCount()
	}
	return EngineStats{
		TotalStreams: len(e.streamLastSeen),
		TotalEmitted: e.totalEmitted,
		TotalEvicted: e.totalEvicted,
		RuleStreams:  ruleStreams,
	}
}

// evictOldestGlobal removes the oldest stream entry when global cap is exceeded.
func (e *EvidenceEngine) evictOldestGlobal() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for key, ts := range e.streamLastSeen {
		if first || ts.Before(oldestTime) {
			oldestKey = key
			oldestTime = ts
			first = false
		}
	}
	if oldestKey != "" {
		delete(e.streamLastSeen, oldestKey)
		for _, rule := range e.rules {
			rule.EvictStream(oldestKey)
		}
		e.totalEvicted++
	}
}
