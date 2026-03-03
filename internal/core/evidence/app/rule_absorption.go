package app

import (
	"math"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

// absStreamState holds per-stream state for absorption detection.
type absStreamState struct {
	volumeRing RingFloat64
	cumVol     float64 // cumulative volume since last price anchor
	priceRef   float64 // reference price when accumulation started
	cooldown   streamEntry
}

// AbsorptionRule detects when large cumulative trade volume occurs
// with minimal price movement — a sign that passive orders are absorbing
// aggressive flow.
type AbsorptionRule struct {
	cfg     RuleConfig
	streams map[string]*absStreamState
}

// NewAbsorptionRule creates an absorption detector.
func NewAbsorptionRule(cfg RuleConfig) *AbsorptionRule {
	return &AbsorptionRule{
		cfg:     cfg,
		streams: make(map[string]*absStreamState),
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
	if st.volumeRing.Len() < 10 {
		st.priceRef = event.TradePrice
		return nil
	}

	meanVol := st.volumeRing.Mean()

	// Set reference price on first meaningful observation
	if st.priceRef == 0 {
		st.priceRef = event.TradePrice
	}

	st.cumVol += event.TradeSize

	// Check price movement from reference
	priceMove := math.Abs(event.TradePrice-st.priceRef) / st.priceRef

	// If price moved significantly (>0.1%), reset accumulation
	if priceMove > 0.001 {
		st.cumVol = 0
		st.priceRef = event.TradePrice
		return nil
	}

	// Absorption: cumulative volume > 2x mean with <0.1% price move
	volRatio := st.cumVol / (meanVol * float64(st.volumeRing.Len()))
	if volRatio < 2.0 {
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

	sev := absSeverity(volRatio)

	return []domain.EvidenceEvent{{
		Kind:        domain.Absorption,
		TsServer:    event.TsServer,
		Venue:       event.Venue,
		Symbol:      event.Instrument,
		Severity:    sev,
		Confidence:  absConfidence(volRatio),
		Features:    []string{"volume_ratio", "cum_volume", "price_move_pct"},
		FeatureVals: []float64{volRatio, cumVolEmitted, priceMove * 100},
		Reason:      "large cumulative volume absorbed with minimal price movement",
		SeqTrigger:  event.Seq,
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
