package app

import (
	"math"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

type lelBookImbalanceState struct {
	consecutive  int
	lastSign     float64
	streakStart  int64
	streakStartT int64
	cooldown     streamEntry
}

// LELBookImbalanceRule detects persistent depth imbalance on one side.
type LELBookImbalanceRule struct {
	cfg                RuleConfig
	imbalanceThreshold float64
	minConsecutive     int
	streams            map[string]*lelBookImbalanceState
}

// NewLELBookImbalanceRule creates a deterministic rule instance.
func NewLELBookImbalanceRule(cfg RuleConfig) *LELBookImbalanceRule {
	return &LELBookImbalanceRule{
		cfg:                cfg,
		imbalanceThreshold: 0.30,
		minConsecutive:     10,
		streams:            make(map[string]*lelBookImbalanceState),
	}
}

func (r *LELBookImbalanceRule) Name() string {
	return string(domain.LiquidityEvidenceTypeBookImbalance)
}

func (r *LELBookImbalanceRule) OnEvent(event domain.LELEvent) []domain.LiquidityEvidence {
	if event.Kind != domain.LELEventKindSnapshot {
		return nil
	}
	key := event.StreamKey()
	st := r.getOrCreate(key)

	imb := DepthImbalance(event.BidDepth, event.AskDepth)
	absImb := math.Abs(imb)
	if absImb < r.imbalanceThreshold {
		st.consecutive = 0
		st.lastSign = 0
		st.streakStart = 0
		st.streakStartT = 0
		return nil
	}

	sign := 1.0
	if imb < 0 {
		sign = -1.0
	}
	if sign == st.lastSign {
		st.consecutive++
	} else {
		st.consecutive = 1
		st.lastSign = sign
		st.streakStart = event.Seq
		st.streakStartT = event.TsServer
	}
	if st.streakStart <= 0 {
		st.streakStart = event.Seq
	}
	if st.streakStartT <= 0 {
		st.streakStartT = event.TsServer
	}
	if st.consecutive < r.minConsecutive {
		return nil
	}
	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	windowMs := event.TsServer - st.streakStartT
	if windowMs <= 0 {
		windowMs = 1
	}
	return []domain.LiquidityEvidence{{
		EvidenceType: domain.LiquidityEvidenceTypeBookImbalance,
		TsIngestMs:   event.TsServer,
		Venue:        event.Venue,
		Symbol:       event.Symbol,
		WindowMs:     windowMs,
		Severity:     bookImbalanceSeverity(st.consecutive),
		Confidence:   bookImbalanceConfidence(st.consecutive),
		Metrics: []domain.LiquidityEvidenceMetric{
			{Key: "consecutive", Value: float64(st.consecutive)},
			{Key: "dominant_side", Value: sign},
			{Key: "imbalance", Value: imb},
		},
		Explain:  []string{"persistent bid/ask depth imbalance detected"},
		Version:  domain.LiquidityEvidenceVersion,
		StreamID: event.StreamID,
		Seq:      event.Seq,
		Watermark: domain.LiquidityInputWatermark{
			SeqStart: st.streakStart,
			SeqEnd:   event.Seq,
		},
	}}
}

func (r *LELBookImbalanceRule) StreamCount() int {
	return len(r.streams)
}

func (r *LELBookImbalanceRule) Reset() {
	r.streams = make(map[string]*lelBookImbalanceState)
}

func (r *LELBookImbalanceRule) EvictStream(key string) {
	delete(r.streams, key)
}

func (r *LELBookImbalanceRule) getOrCreate(key string) *lelBookImbalanceState {
	if st, ok := r.streams[key]; ok {
		return st
	}
	if len(r.streams) >= r.cfg.MaxStreams {
		evictOldest(r.streams)
	}
	st := &lelBookImbalanceState{}
	r.streams[key] = st
	return st
}

func bookImbalanceSeverity(consecutive int) domain.LiquidityEvidenceSeverity {
	switch {
	case consecutive >= 30:
		return domain.LiquidityEvidenceSeverityCritical
	case consecutive >= 20:
		return domain.LiquidityEvidenceSeverityHigh
	default:
		return domain.LiquidityEvidenceSeverityMedium
	}
}

func bookImbalanceConfidence(consecutive int) float64 {
	switch {
	case consecutive >= 30:
		return 0.95
	case consecutive >= 20:
		return 0.85
	default:
		return 0.70
	}
}
