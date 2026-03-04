package app

import (
	"strings"
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
	BufferCapPerKind  int
	DecayHalfLife     time.Duration
	Now               func() time.Time // for deterministic tests
}

// DefaultEngineConfig returns production defaults.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MaxStreamsPerRule: 256,
		MaxStreamsGlobal:  1024,
		StreamTTL:         10 * time.Minute,
		SweepInterval:     1 * time.Minute,
		BufferCapPerKind:  1000,
		DecayHalfLife:     1 * time.Minute,
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
	buffer         *domain.EvidenceBuffer
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
	if cfg.BufferCapPerKind <= 0 {
		cfg.BufferCapPerKind = 1000
	}
	if cfg.DecayHalfLife <= 0 {
		cfg.DecayHalfLife = time.Minute
	}
	policy, p := domain.NewEvidenceBufferPolicy(cfg.BufferCapPerKind)
	if p != nil {
		policy = domain.EvidenceBufferPolicy{MaxPerKind: 1000}
	}
	return &EvidenceEngine{
		cfg:            cfg,
		rules:          rules,
		buffer:         domain.NewEvidenceBuffer(policy),
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
			age := eventAge(now, events[i].TsServer)
			events[i].Confidence = domain.ApplyConfidenceDecay(events[i].Confidence, age, e.cfg.DecayHalfLife)
			overwritten, p := e.buffer.Push(events[i])
			if p != nil {
				continue
			}
			metrics.SetEvidenceBufferEntries(string(events[i].Kind), e.buffer.Size(events[i].Kind))
			if overwritten {
				metrics.IncEvidenceBufferOverwrites(string(events[i].Kind))
			}
			metrics.IncEvidenceEmitted(string(events[i].Kind), string(events[i].Severity))
			result = append(result, events[i])
		}
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
		if first || ts.Before(oldestTime) || (ts.Equal(oldestTime) && strings.Compare(key, oldestKey) < 0) {
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

func eventAge(now time.Time, tsServerMs int64) time.Duration {
	if tsServerMs <= 0 {
		return 0
	}
	nowMs := now.UnixMilli()
	if nowMs <= tsServerMs {
		return 0
	}
	return time.Duration(nowMs-tsServerMs) * time.Millisecond
}
