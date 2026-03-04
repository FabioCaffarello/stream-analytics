package app

import "github.com/market-raccoon/internal/core/evidence/domain"

type lelThinningState struct {
	depthRing RingFloat64
	seqRing   RingInt64
	tsRing    RingInt64
	cooldown  streamEntry
}

// LELThinningRule detects statistically significant total-depth thinning.
type LELThinningRule struct {
	cfg        RuleConfig
	minSamples int
	minDropPct float64
	maxZScore  float64
	streams    map[string]*lelThinningState
}

// NewLELThinningRule creates a deterministic rule instance.
func NewLELThinningRule(cfg RuleConfig) *LELThinningRule {
	return &LELThinningRule{
		cfg:        cfg,
		minSamples: 10,
		minDropPct: 0.30,
		maxZScore:  -2.0,
		streams:    make(map[string]*lelThinningState),
	}
}

func (r *LELThinningRule) Name() string { return string(domain.LiquidityEvidenceTypeThinning) }

func (r *LELThinningRule) OnEvent(event domain.LELEvent) []domain.LiquidityEvidence {
	if event.Kind != domain.LELEventKindSnapshot {
		return nil
	}
	totalDepth := DepthTotal(event.BidDepth, event.AskDepth)
	if totalDepth < 0 {
		return nil
	}

	key := event.StreamKey()
	st := r.getOrCreate(key)
	st.depthRing.Push(totalDepth)
	st.seqRing.Push(event.Seq)
	st.tsRing.Push(event.TsServer)

	if st.depthRing.Len() < r.minSamples {
		return nil
	}
	mean := st.depthRing.Mean()
	stdDev := st.depthRing.StdDev()
	if mean <= 0 {
		return nil
	}
	dropPct := (mean - totalDepth) / mean
	z := ZScore(totalDepth, mean, stdDev)
	if z > r.maxZScore || dropPct < r.minDropPct {
		return nil
	}
	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	windowMs := event.TsServer - st.tsRing.Oldest()
	if windowMs <= 0 {
		windowMs = 1
	}
	return []domain.LiquidityEvidence{{
		EvidenceType: domain.LiquidityEvidenceTypeThinning,
		TsIngestMs:   event.TsServer,
		Venue:        event.Venue,
		Symbol:       event.Symbol,
		WindowMs:     windowMs,
		Severity:     thinningSeverity(dropPct),
		Confidence:   thinningConfidence(dropPct),
		Metrics: []domain.LiquidityEvidenceMetric{
			{Key: "depth_drop_pct", Value: dropPct * 100},
			{Key: "mean_depth", Value: mean},
			{Key: "total_depth", Value: totalDepth},
		},
		Explain:  []string{"liquidity withdrawal below statistical norm"},
		Version:  domain.LiquidityEvidenceVersion,
		StreamID: event.StreamID,
		Seq:      event.Seq,
		Watermark: domain.LiquidityInputWatermark{
			SeqStart: st.seqRing.Oldest(),
			SeqEnd:   event.Seq,
		},
	}}
}

func (r *LELThinningRule) StreamCount() int { return len(r.streams) }

func (r *LELThinningRule) Reset() {
	r.streams = make(map[string]*lelThinningState)
}

func (r *LELThinningRule) EvictStream(key string) {
	delete(r.streams, key)
}

func (r *LELThinningRule) getOrCreate(key string) *lelThinningState {
	if st, ok := r.streams[key]; ok {
		return st
	}
	if len(r.streams) >= r.cfg.MaxStreams {
		evictOldest(r.streams)
	}
	st := &lelThinningState{}
	r.streams[key] = st
	return st
}

func thinningSeverity(dropPct float64) domain.LiquidityEvidenceSeverity {
	switch {
	case dropPct >= 0.70:
		return domain.LiquidityEvidenceSeverityCritical
	case dropPct >= 0.50:
		return domain.LiquidityEvidenceSeverityHigh
	default:
		return domain.LiquidityEvidenceSeverityMedium
	}
}

func thinningConfidence(dropPct float64) float64 {
	switch {
	case dropPct >= 0.70:
		return 0.95
	case dropPct >= 0.50:
		return 0.85
	default:
		return 0.70
	}
}
