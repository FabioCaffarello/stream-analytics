package app

import (
	"math"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

// absStreamState holds per-stream state for absorption detection.
type absStreamState struct {
	volumeRing           RingFloat64
	cumVol               float64 // cumulative volume since last price anchor
	priceRef             float64 // reference price when accumulation started
	accumulationStartSeq int64
	cooldown             streamEntry
}

// AbsorptionThresholds configures deterministic absorption detection.
type AbsorptionThresholds struct {
	MinSamples      int
	MaxPriceMovePct float64
	MinVolumeRatio  float64
}

func defaultAbsorptionThresholds() AbsorptionThresholds {
	return AbsorptionThresholds{
		MinSamples:      10,
		MaxPriceMovePct: 0.1,
		MinVolumeRatio:  2.0,
	}
}

// AbsorptionRule detects when large cumulative trade volume occurs
// with minimal price movement — a sign that passive orders are absorbing
// aggressive flow.
type AbsorptionRule struct {
	cfg        RuleConfig
	thresholds AbsorptionThresholds
	streams    map[string]*absStreamState
}

// NewAbsorptionRule creates an absorption detector.
func NewAbsorptionRule(cfg RuleConfig) *AbsorptionRule {
	return &AbsorptionRule{
		cfg:        cfg,
		thresholds: defaultAbsorptionThresholds(),
		streams:    make(map[string]*absStreamState),
	}
}

func (r *AbsorptionRule) Name() string { return string(domain.Absorption) }

func (r *AbsorptionRule) OnEvent(event domain.RuleEvent) []domain.EvidenceEvent {
	if event.Kind != domain.EventKindTrade {
		return nil
	}
	if event.TradePrice <= 0 || event.TradeSize <= 0 {
		return nil
	}

	key := event.StreamKey()
	st := r.getOrCreate(key)

	st.volumeRing.Push(event.TradeSize)
	if st.volumeRing.Len() < r.thresholds.MinSamples {
		st.priceRef = event.TradePrice
		st.accumulationStartSeq = event.Seq
		return nil
	}

	meanVol := st.volumeRing.Mean()

	// Set reference price on first meaningful observation
	if st.priceRef == 0 {
		st.priceRef = event.TradePrice
		st.accumulationStartSeq = event.Seq
	}

	st.cumVol += event.TradeSize

	// Check price movement from reference
	priceMove := math.Abs(event.TradePrice-st.priceRef) / st.priceRef

	if priceMove*100 > r.thresholds.MaxPriceMovePct {
		st.cumVol = 0
		st.priceRef = event.TradePrice
		st.accumulationStartSeq = event.Seq
		return nil
	}

	// Absorption: cumulative volume > threshold with compressed price move.
	volRatio := st.cumVol / (meanVol * float64(st.volumeRing.Len()))
	if volRatio < r.thresholds.MinVolumeRatio {
		return nil
	}

	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	// Reset after emission
	cumVolEmitted := st.cumVol
	st.cumVol = 0
	st.priceRef = event.TradePrice
	startSeq := st.accumulationStartSeq
	if startSeq <= 0 {
		startSeq = event.Seq
	}
	st.accumulationStartSeq = event.Seq

	sev := absSeverity(volRatio)

	return []domain.EvidenceEvent{{
		Type:       domain.Absorption,
		TsServer:   event.TsServer,
		Venue:      event.Venue,
		Symbol:     event.Symbol,
		StreamID:   resolveStreamID(event),
		Seq:        event.Seq,
		Severity:   sev,
		Confidence: absConfidence(volRatio),
		Features: domain.FeaturesFromMap(map[string]float64{
			"cum_volume":     cumVolEmitted,
			"price_move_pct": priceMove * 100,
			"volume_ratio":   volRatio,
		}),
		Explanation: "large cumulative volume absorbed with minimal price movement",
		RuleVersion: domain.RuleVersionV0,
		InputWatermark: domain.InputWatermark{
			SeqStart: startSeq,
			SeqEnd:   event.Seq,
		},
	}}
}

func (r *AbsorptionRule) StreamCount() int { return len(r.streams) }

func (r *AbsorptionRule) Reset() {
	r.streams = make(map[string]*absStreamState)
}

func (r *AbsorptionRule) EvictStream(key string) {
	delete(r.streams, key)
}

func (r *AbsorptionRule) getOrCreate(key string) *absStreamState {
	st, ok := r.streams[key]
	if ok {
		return st
	}
	if len(r.streams) >= r.cfg.MaxStreams {
		evictOldest(r.streams)
	}
	st = &absStreamState{}
	r.streams[key] = st
	return st
}

func absSeverity(ratio float64) domain.Severity {
	switch {
	case ratio >= 8.0:
		return domain.SeverityCritical
	case ratio >= 4.0:
		return domain.SeverityHigh
	default:
		return domain.SeverityMedium
	}
}

func absConfidence(ratio float64) float64 {
	switch {
	case ratio >= 8.0:
		return 0.95
	case ratio >= 4.0:
		return 0.85
	default:
		return 0.70
	}
}
