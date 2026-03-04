package app

import (
	"strings"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

// spreadStreamState holds per-stream rolling state for spread explosion detection.
type spreadStreamState struct {
	ring     RingFloat64
	seqRing  RingInt64
	cooldown streamEntry
}

// SpreadExplosionThresholds configures detection thresholds for spread explosions.
type SpreadExplosionThresholds struct {
	MinSamples   int
	MinZScore    float64
	MinSpreadBps float64
}

func defaultSpreadExplosionThresholds() SpreadExplosionThresholds {
	return SpreadExplosionThresholds{
		MinSamples:   10,
		MinZScore:    2.5,
		MinSpreadBps: 10,
	}
}

// SpreadExplosionRule detects when the bid-ask spread suddenly widens
// beyond statistical norms (z-score based on rolling history).
type SpreadExplosionRule struct {
	cfg        RuleConfig
	thresholds SpreadExplosionThresholds
	streams    map[string]*spreadStreamState
}

// NewSpreadExplosionRule creates a spread explosion detector.
func NewSpreadExplosionRule(cfg RuleConfig) *SpreadExplosionRule {
	return &SpreadExplosionRule{
		cfg:        cfg,
		thresholds: defaultSpreadExplosionThresholds(),
		streams:    make(map[string]*spreadStreamState),
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
	st.seqRing.Push(event.Seq)

	if st.ring.Len() < r.thresholds.MinSamples {
		return nil
	}

	mean := st.ring.Mean()
	stddev := st.ring.StdDev()
	z := ZScore(spreadBps, mean, stddev)

	if z <= r.thresholds.MinZScore || spreadBps <= r.thresholds.MinSpreadBps {
		return nil
	}

	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	sev := severityFromZ(z)

	return []domain.EvidenceEvent{{
		Type:       domain.SpreadExplosion,
		TsServer:   event.TsServer,
		Venue:      event.Venue,
		Symbol:     event.Symbol,
		StreamID:   resolveStreamID(event),
		Seq:        event.Seq,
		Severity:   sev,
		Confidence: confidenceFromZ(z),
		Features: domain.FeaturesFromMap(map[string]float64{
			"mean_bps":   mean,
			"spread_bps": spreadBps,
			"z_score":    z,
		}),
		Explanation: "spread z-score exceeded threshold",
		RuleVersion: domain.RuleVersionV0,
		InputWatermark: domain.InputWatermark{
			SeqStart: st.seqRing.Oldest(),
			SeqEnd:   event.Seq,
		},
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
