package app

import (
	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

// liqStreamState holds per-stream rolling state for liquidity thinning.
type liqStreamState struct {
	ring     RingFloat64
	seqRing  RingInt64
	cooldown streamEntry
}

// LiquidityThinningThresholds configures detection thresholds for thinning events.
type LiquidityThinningThresholds struct {
	MinSamples int
	MinDropPct float64
	MaxZScore  float64
}

func defaultLiquidityThinningThresholds() LiquidityThinningThresholds {
	return LiquidityThinningThresholds{
		MinSamples: 10,
		MinDropPct: 0.30,
		MaxZScore:  -2.0,
	}
}

// LiquidityThinningRule detects when aggregate order book depth drops
// significantly below its rolling average (>2σ AND >30% drop).
type LiquidityThinningRule struct {
	cfg        RuleConfig
	thresholds LiquidityThinningThresholds
	streams    map[string]*liqStreamState
}

// NewLiquidityThinningRule creates a liquidity thinning detector.
func NewLiquidityThinningRule(cfg RuleConfig) *LiquidityThinningRule {
	return &LiquidityThinningRule{
		cfg:        cfg,
		thresholds: defaultLiquidityThinningThresholds(),
		streams:    make(map[string]*liqStreamState),
	}
}

func (r *LiquidityThinningRule) Name() string { return string(domain.LiquidityThinning) }

func (r *LiquidityThinningRule) OnEvent(event domain.RuleEvent) []domain.EvidenceEvent {
	if event.Kind != domain.EventKindBook {
		return nil
	}

	totalDepth := event.BidDepth + event.AskDepth
	if totalDepth < 0 {
		return nil
	}

	key := event.StreamKey()
	st := r.getOrCreate(key)

	st.ring.Push(totalDepth)
	st.seqRing.Push(event.Seq)

	if st.ring.Len() < r.thresholds.MinSamples {
		return nil
	}

	mean := st.ring.Mean()
	stddev := st.ring.StdDev()

	if mean <= 0 {
		return nil
	}

	dropPct := (mean - totalDepth) / mean
	z := ZScore(totalDepth, mean, stddev)

	if z > r.thresholds.MaxZScore || dropPct < r.thresholds.MinDropPct {
		return nil
	}

	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	sev := liqSeverity(dropPct)

	return []domain.EvidenceEvent{{
		Type:       domain.LiquidityThinning,
		TsServer:   event.TsServer,
		Venue:      event.Venue,
		Symbol:     event.Symbol,
		StreamID:   resolveStreamID(event),
		Seq:        event.Seq,
		Severity:   sev,
		Confidence: liqConfidence(dropPct),
		Features: domain.FeaturesFromMap(map[string]float64{
			"depth_drop_pct": dropPct * 100,
			"mean_depth":     mean,
			"total_depth":    totalDepth,
		}),
		Explanation: "aggregate depth dropped significantly below rolling mean",
		RuleVersion: domain.RuleVersionV0,
		InputWatermark: domain.InputWatermark{
			SeqStart: st.seqRing.Oldest(),
			SeqEnd:   event.Seq,
		},
	}}
}

func (r *LiquidityThinningRule) StreamCount() int { return len(r.streams) }

func (r *LiquidityThinningRule) Reset() {
	r.streams = make(map[string]*liqStreamState)
}

func (r *LiquidityThinningRule) EvictStream(key string) {
	delete(r.streams, key)
}

func (r *LiquidityThinningRule) getOrCreate(key string) *liqStreamState {
	st, ok := r.streams[key]
	if ok {
		return st
	}
	if len(r.streams) >= r.cfg.MaxStreams {
		evictOldest(r.streams)
	}
	st = &liqStreamState{}
	r.streams[key] = st
	return st
}

func liqSeverity(dropPct float64) domain.Severity {
	switch {
	case dropPct >= 0.70:
		return domain.SeverityCritical
	case dropPct >= 0.50:
		return domain.SeverityHigh
	default:
		return domain.SeverityMedium
	}
}

func liqConfidence(dropPct float64) float64 {
	switch {
	case dropPct >= 0.70:
		return 0.95
	case dropPct >= 0.50:
		return 0.85
	default:
		return 0.70
	}
}
