package app

import "github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"

type lelSweepState struct {
	lastBidLevels int
	lastAskLevels int
	lastBidDepth  float64
	lastAskDepth  float64
	lastSeq       int64
	lastTs        int64
	cooldown      streamEntry
}

// LELSweepRule detects rapid single-side level consumption.
type LELSweepRule struct {
	cfg             RuleConfig
	minLevelDrop    int
	minDepthDropPct float64
	streams         map[string]*lelSweepState
}

// NewLELSweepRule creates a deterministic rule instance.
func NewLELSweepRule(cfg RuleConfig) *LELSweepRule {
	return &LELSweepRule{
		cfg:             cfg,
		minLevelDrop:    5,
		minDepthDropPct: 0.40,
		streams:         make(map[string]*lelSweepState),
	}
}

func (r *LELSweepRule) Name() string { return string(domain.LiquidityEvidenceTypeSweep) }

func (r *LELSweepRule) OnEvent(event domain.LELEvent) []domain.LiquidityEvidence {
	if event.Kind != domain.LELEventKindSnapshot {
		return nil
	}
	key := event.StreamKey()
	st := r.getOrCreate(key)

	if st.lastSeq <= 0 {
		r.update(st, event)
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
	case bidLevelDrop >= r.minLevelDrop && bidDepthDrop >= r.minDepthDropPct && bidLevelDrop >= askLevelDrop:
		side = "bid"
		levelDrop = bidLevelDrop
		depthDrop = bidDepthDrop
	case askLevelDrop >= r.minLevelDrop && askDepthDrop >= r.minDepthDropPct:
		side = "ask"
		levelDrop = askLevelDrop
		depthDrop = askDepthDrop
	}

	prevSeq := st.lastSeq
	prevTs := st.lastTs
	r.update(st, event)
	if side == "" {
		return nil
	}
	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	windowMs := event.TsServer - prevTs
	if windowMs <= 0 {
		windowMs = 1
	}
	return []domain.LiquidityEvidence{{
		EvidenceType: domain.LiquidityEvidenceTypeSweep,
		TsIngestMs:   event.TsServer,
		Venue:        event.Venue,
		Symbol:       event.Symbol,
		WindowMs:     windowMs,
		Severity:     domain.LiquidityEvidenceSeverity(sweepSeverity(levelDrop, depthDrop)),
		Confidence:   sweepConfidence(levelDrop, depthDrop),
		Metrics: []domain.LiquidityEvidenceMetric{
			{Key: "depth_drop_pct", Value: depthDrop * 100},
			{Key: "level_drop", Value: float64(levelDrop)},
			{Key: "side", Value: sideToNumeric(side)},
		},
		Explain:  []string{"rapid level consumption detected on " + side + " side"},
		Version:  domain.LiquidityEvidenceVersion,
		StreamID: event.StreamID,
		Seq:      event.Seq,
		Watermark: domain.LiquidityInputWatermark{
			SeqStart: prevSeq,
			SeqEnd:   event.Seq,
		},
	}}
}

func (r *LELSweepRule) StreamCount() int { return len(r.streams) }

func (r *LELSweepRule) Reset() { r.streams = make(map[string]*lelSweepState) }

func (r *LELSweepRule) EvictStream(key string) { delete(r.streams, key) }

func (r *LELSweepRule) getOrCreate(key string) *lelSweepState {
	if st, ok := r.streams[key]; ok {
		return st
	}
	if len(r.streams) >= r.cfg.MaxStreams {
		evictOldest(r.streams)
	}
	st := &lelSweepState{}
	r.streams[key] = st
	return st
}

func (r *LELSweepRule) update(st *lelSweepState, event domain.LELEvent) {
	st.lastBidLevels = event.BidLevels
	st.lastAskLevels = event.AskLevels
	st.lastBidDepth = event.BidDepth
	st.lastAskDepth = event.AskDepth
	st.lastSeq = event.Seq
	st.lastTs = event.TsServer
}
