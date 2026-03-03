package app

import (
	"strings"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

// spreadStreamState holds per-stream rolling state for spread explosion detection.
type spreadStreamState struct {
	ring     RingFloat64
	cooldown streamEntry
}

// SpreadExplosionRule detects when the bid-ask spread suddenly widens
// beyond statistical norms (z-score based on rolling history).
type SpreadExplosionRule struct {
	cfg     RuleConfig
	streams map[string]*spreadStreamState
}

// NewSpreadExplosionRule creates a spread explosion detector.
func NewSpreadExplosionRule(cfg RuleConfig) *SpreadExplosionRule {
	return &SpreadExplosionRule{
		cfg:     cfg,
		streams: make(map[string]*spreadStreamState),
	}
}

func (r *SpreadExplosionRule) Name() string { return string(domain.SpreadExplosion) }

func (r *SpreadExplosionRule) OnEvent(event domain.RuleEvent) []domain.EvidenceEvent {
	if event.Kind != domain.EventKindBook {
		return nil
	}
	if event.BestBid <= 0 || event.BestAsk <= 0 || event.BestAsk <= event.BestBid {
		return nil
	}

	key := event.StreamKey()
	st := r.getOrCreate(key)

	spreadBps := SpreadBps(event.BestBid, event.BestAsk)
	st.ring.Push(spreadBps)

	// Need at least 10 observations for meaningful statistics
	if st.ring.Len() < 10 {
		return nil
	}

	mean := st.ring.Mean()
	stddev := st.ring.StdDev()
	z := ZScore(spreadBps, mean, stddev)

	// Threshold: z > 2.5 AND absolute spread > 10 bps
	if z <= 2.5 || spreadBps <= 10 {
		return nil
	}

	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	sev := severityFromZ(z)

	return []domain.EvidenceEvent{{
		Kind:        domain.SpreadExplosion,
		TsServer:    event.TsServer,
		Venue:       event.Venue,
		Symbol:      event.Instrument,
		Severity:    sev,
		Confidence:  confidenceFromZ(z),
		Features:    []string{"spread_bps", "z_score", "mean_bps"},
		FeatureVals: []float64{spreadBps, z, mean},
		Reason:      "spread z-score exceeded threshold",
		SeqTrigger:  event.Seq,
	}}
}

func (r *SpreadExplosionRule) StreamCount() int { return len(r.streams) }

func (r *SpreadExplosionRule) Reset() {
	r.streams = make(map[string]*spreadStreamState)
}

func (r *SpreadExplosionRule) EvictStream(key string) {
	delete(r.streams, key)
}

func (r *SpreadExplosionRule) getOrCreate(key string) *spreadStreamState {
	st, ok := r.streams[key]
	if ok {
		return st
	}
	if len(r.streams) >= r.cfg.MaxStreams {
		evictOldest(r.streams)
	}
	st = &spreadStreamState{}
	r.streams[key] = st
	return st
}

// severityFromZ maps z-score to severity.
func severityFromZ(z float64) domain.Severity {
	switch {
	case z >= 5.0:
		return domain.SeverityCritical
	case z >= 3.5:
		return domain.SeverityHigh
	default:
		return domain.SeverityMedium
	}
}

// confidenceFromZ maps z-score to confidence in [0,1].
func confidenceFromZ(z float64) float64 {
	switch {
	case z >= 5.0:
		return 0.95
	case z >= 3.5:
		return 0.85
	default:
		return 0.70
	}
}

// evictOldest removes a deterministic key to keep bounded-state behavior reproducible.
// We currently select the lexicographically smallest stream key.
func evictOldest[V any](m map[string]*V) {
	var victim string
	var found bool
	for k := range m {
		if !found || strings.Compare(k, victim) < 0 {
			victim = k
			found = true
		}
	}
	if found {
		delete(m, victim)
	}
}
