package app

import (
	"github.com/market-raccoon/internal/core/evidence/domain"
)

// liqStreamState holds per-stream rolling state for liquidity thinning.
type liqStreamState struct {
	ring     RingFloat64
	cooldown streamEntry
}

// LiquidityThinningRule detects when aggregate order book depth drops
// significantly below its rolling average (>2σ AND >30% drop).
type LiquidityThinningRule struct {
	cfg     RuleConfig
	streams map[string]*liqStreamState
}

// NewLiquidityThinningRule creates a liquidity thinning detector.
func NewLiquidityThinningRule(cfg RuleConfig) *LiquidityThinningRule {
	return &LiquidityThinningRule{
		cfg:     cfg,
		streams: make(map[string]*liqStreamState),
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

	if st.ring.Len() < 10 {
		return nil
	}

	mean := st.ring.Mean()
	stddev := st.ring.StdDev()

	if mean <= 0 {
		return nil
	}

	dropPct := (mean - totalDepth) / mean
	z := ZScore(totalDepth, mean, stddev)

	// Detect: z < -2 (depth is 2σ below mean) AND drop > 30%
	if z > -2 || dropPct < 0.30 {
		return nil
	}

	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	sev := liqSeverity(dropPct)

	return []domain.EvidenceEvent{{
		Kind:        domain.LiquidityThinning,
		TsServer:    event.TsServer,
		Venue:       event.Venue,
		Symbol:      event.Instrument,
		Severity:    sev,
		Confidence:  liqConfidence(dropPct),
		Features:    []string{"depth_drop_pct", "total_depth", "mean_depth"},
		FeatureVals: []float64{dropPct * 100, totalDepth, mean},
		Reason:      "aggregate depth dropped significantly below rolling mean",
		SeqTrigger:  event.Seq,
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
