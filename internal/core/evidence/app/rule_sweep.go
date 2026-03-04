package app

import "github.com/market-raccoon/internal/core/evidence/domain"

// sweepStreamState tracks prior book depth/levels for sweep detection.
type sweepStreamState struct {
	lastBidLevels int
	lastAskLevels int
	lastBidDepth  float64
	lastAskDepth  float64
	cooldown      streamEntry
}

// SweepRule detects abrupt multi-level book depletion on one side.
//
// Heuristic (deterministic):
//   - level drop >= 5 on one side
//   - depth drop >= 40% on the same side
type SweepRule struct {
	cfg            RuleConfig
	minLevelDrop   int
	minDepthDropPc float64
	streams        map[string]*sweepStreamState
}

// NewSweepRule creates a sweep detector.
func NewSweepRule(cfg RuleConfig) *SweepRule {
	return &SweepRule{
		cfg:            cfg,
		minLevelDrop:   5,
		minDepthDropPc: 0.40,
		streams:        make(map[string]*sweepStreamState),
	}
}

func (r *SweepRule) Name() string { return string(domain.Sweep) }

func (r *SweepRule) OnEvent(event domain.RuleEvent) []domain.EvidenceEvent {
	if event.Kind != domain.EventKindBook {
		return nil
	}

	key := event.StreamKey()
	st := r.getOrCreate(key)
	if st.lastBidLevels == 0 && st.lastAskLevels == 0 {
		r.updateState(st, event)
		return nil
	}

	bidLevelDrop := st.lastBidLevels - event.BidLevels
	askLevelDrop := st.lastAskLevels - event.AskLevels
	bidDepthDrop := dropPct(st.lastBidDepth, event.BidDepth)
	askDepthDrop := dropPct(st.lastAskDepth, event.AskDepth)

	side := ""
	levelDrop := 0
	depthDrop := 0.0
	switch {
	case bidLevelDrop >= r.minLevelDrop && bidDepthDrop >= r.minDepthDropPc && bidLevelDrop >= askLevelDrop:
		side = "bid"
		levelDrop = bidLevelDrop
		depthDrop = bidDepthDrop
	case askLevelDrop >= r.minLevelDrop && askDepthDrop >= r.minDepthDropPc:
		side = "ask"
		levelDrop = askLevelDrop
		depthDrop = askDepthDrop
	}

	r.updateState(st, event)
	if side == "" {
		return nil
	}
	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	return []domain.EvidenceEvent{{
		Kind:       domain.Sweep,
		TsServer:   event.TsServer,
		Venue:      event.Venue,
		Symbol:     event.Instrument,
		Severity:   sweepSeverity(levelDrop, depthDrop),
		Confidence: sweepConfidence(levelDrop, depthDrop),
		Features: []string{
			"level_drop",
			"depth_drop_pct",
			"side",
		},
		FeatureVals: []float64{
			float64(levelDrop),
			depthDrop * 100,
			sideToNumeric(side),
		},
		Reason:     "order book " + side + " side depleted across multiple levels",
		SeqTrigger: event.Seq,
	}}
}

func (r *SweepRule) StreamCount() int { return len(r.streams) }

func (r *SweepRule) Reset() { r.streams = make(map[string]*sweepStreamState) }

func (r *SweepRule) EvictStream(key string) { delete(r.streams, key) }

func (r *SweepRule) getOrCreate(key string) *sweepStreamState {
	st, ok := r.streams[key]
	if ok {
		return st
	}
	if len(r.streams) >= r.cfg.MaxStreams {
		evictOldest(r.streams)
	}
	st = &sweepStreamState{}
	r.streams[key] = st
	return st
}

func (r *SweepRule) updateState(st *sweepStreamState, event domain.RuleEvent) {
	st.lastBidLevels = event.BidLevels
	st.lastAskLevels = event.AskLevels
	st.lastBidDepth = event.BidDepth
	st.lastAskDepth = event.AskDepth
}

func dropPct(prev, current float64) float64 {
	if prev <= 0 || current >= prev {
		return 0
	}
	return (prev - current) / prev
}

func sideToNumeric(side string) float64 {
	if side == "ask" {
		return -1
	}
	return 1
}

func sweepSeverity(levelDrop int, depthDrop float64) domain.Severity {
	switch {
	case levelDrop >= 12 || depthDrop >= 0.75:
		return domain.SeverityCritical
	case levelDrop >= 8 || depthDrop >= 0.60:
		return domain.SeverityHigh
	default:
		return domain.SeverityMedium
	}
}

func sweepConfidence(levelDrop int, depthDrop float64) float64 {
	switch {
	case levelDrop >= 12 || depthDrop >= 0.75:
		return 0.95
	case levelDrop >= 8 || depthDrop >= 0.60:
		return 0.85
	default:
		return 0.70
	}
}
