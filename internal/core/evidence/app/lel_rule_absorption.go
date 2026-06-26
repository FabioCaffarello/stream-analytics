package app

import "github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"

type lelAbsorptionState struct {
	volumeRing RingFloat64
	lastSpread float64
	hasSpread  bool
	cooldown   streamEntry
}

// LELAbsorptionRule detects large tape volume absorbed while spread stays tight.
type LELAbsorptionRule struct {
	cfg            RuleConfig
	minSamples     int
	maxSpreadBPS   float64
	minVolumeRatio float64
	streams        map[string]*lelAbsorptionState
}

// NewLELAbsorptionRule creates a deterministic rule instance.
func NewLELAbsorptionRule(cfg RuleConfig) *LELAbsorptionRule {
	return &LELAbsorptionRule{
		cfg:            cfg,
		minSamples:     10,
		maxSpreadBPS:   50,
		minVolumeRatio: 2.0,
		streams:        make(map[string]*lelAbsorptionState),
	}
}

func (r *LELAbsorptionRule) Name() string { return string(domain.LiquidityEvidenceTypeAbsorption) }

func (r *LELAbsorptionRule) OnEvent(event domain.LELEvent) []domain.LiquidityEvidence {
	key := event.StreamKey()
	st := r.getOrCreate(key)

	switch event.Kind {
	case domain.LELEventKindSnapshot:
		if event.SpreadBPS > 0 {
			st.lastSpread = event.SpreadBPS
			st.hasSpread = true
		} else if event.BestBid > 0 && event.BestAsk > 0 {
			st.lastSpread = SpreadBps(event.BestBid, event.BestAsk)
			st.hasSpread = true
		}
		return nil
	case domain.LELEventKindTape:
		// keep evaluating below.
	default:
		return nil
	}

	if event.TotalVolume <= 0 {
		return nil
	}
	st.volumeRing.Push(event.TotalVolume)
	if st.volumeRing.Len() < r.minSamples {
		return nil
	}
	if !st.hasSpread {
		return nil
	}
	if st.lastSpread > r.maxSpreadBPS {
		return nil
	}
	meanVol := st.volumeRing.Mean()
	volRatio := VolumeRatio(event.TotalVolume, meanVol)
	if volRatio < r.minVolumeRatio {
		return nil
	}
	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	windowMs := event.WindowEndTs - event.WindowStartTs
	if windowMs <= 0 {
		windowMs = 1
	}

	return []domain.LiquidityEvidence{{
		EvidenceType: domain.LiquidityEvidenceTypeAbsorption,
		TsIngestMs:   event.TsServer,
		Venue:        event.Venue,
		Symbol:       event.Symbol,
		WindowMs:     windowMs,
		Severity:     absorptionSeverity(volRatio),
		Confidence:   absorptionConfidence(volRatio),
		Metrics: []domain.LiquidityEvidenceMetric{
			{Key: "cum_volume", Value: event.TotalVolume},
			{Key: "spread_bps", Value: st.lastSpread},
			{Key: "volume_ratio", Value: volRatio},
		},
		Explain:  []string{"large volume absorbed within tight spread regime"},
		Version:  domain.LiquidityEvidenceVersion,
		StreamID: event.StreamID,
		Seq:      event.Seq,
		Watermark: domain.LiquidityInputWatermark{
			SeqStart: event.Seq,
			SeqEnd:   event.Seq,
		},
	}}
}

func (r *LELAbsorptionRule) StreamCount() int { return len(r.streams) }

func (r *LELAbsorptionRule) Reset() {
	r.streams = make(map[string]*lelAbsorptionState)
}

func (r *LELAbsorptionRule) EvictStream(key string) {
	delete(r.streams, key)
}

func (r *LELAbsorptionRule) getOrCreate(key string) *lelAbsorptionState {
	if st, ok := r.streams[key]; ok {
		return st
	}
	if len(r.streams) >= r.cfg.MaxStreams {
		evictOldest(r.streams)
	}
	st := &lelAbsorptionState{}
	r.streams[key] = st
	return st
}

func absorptionSeverity(ratio float64) domain.LiquidityEvidenceSeverity {
	switch {
	case ratio >= 8:
		return domain.LiquidityEvidenceSeverityCritical
	case ratio >= 4:
		return domain.LiquidityEvidenceSeverityHigh
	default:
		return domain.LiquidityEvidenceSeverityMedium
	}
}

func absorptionConfidence(ratio float64) float64 {
	switch {
	case ratio >= 8:
		return 0.95
	case ratio >= 4:
		return 0.85
	default:
		return 0.70
	}
}
