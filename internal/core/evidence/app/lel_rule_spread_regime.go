package app

import (
	"fmt"

	"github.com/FabioCaffarello/stream-analytics/internal/core/evidence/domain"
)

const (
	lelRegimeTight = 1
	lelRegimeNorm  = 2
	lelRegimeWide  = 3
	lelRegimeBlown = 4
)

type lelSpreadRegimeState struct {
	spreadRing RingFloat64
	seqRing    RingInt64
	tsRing     RingInt64
	lastRegime int
	cooldown   streamEntry
}

// LELSpreadRegimeRule detects spread regime transitions under z-score stress.
type LELSpreadRegimeRule struct {
	cfg          RuleConfig
	minSamples   int
	minZScore    float64
	minSpreadBPS float64
	streams      map[string]*lelSpreadRegimeState
}

// NewLELSpreadRegimeRule creates a deterministic rule instance.
func NewLELSpreadRegimeRule(cfg RuleConfig) *LELSpreadRegimeRule {
	return &LELSpreadRegimeRule{
		cfg:          cfg,
		minSamples:   10,
		minZScore:    2.5,
		minSpreadBPS: 10.0,
		streams:      make(map[string]*lelSpreadRegimeState),
	}
}

func (r *LELSpreadRegimeRule) Name() string { return string(domain.LiquidityEvidenceTypeSpreadRegime) }

func (r *LELSpreadRegimeRule) OnEvent(event domain.LELEvent) []domain.LiquidityEvidence {
	if event.Kind != domain.LELEventKindSnapshot {
		return nil
	}

	spreadBPS := event.SpreadBPS
	if spreadBPS <= 0 && event.BestBid > 0 && event.BestAsk > event.BestBid {
		spreadBPS = SpreadBps(event.BestBid, event.BestAsk)
	}
	if spreadBPS <= 0 {
		return nil
	}

	key := event.StreamKey()
	st := r.getOrCreate(key)
	st.spreadRing.Push(spreadBPS)
	st.seqRing.Push(event.Seq)
	st.tsRing.Push(event.TsServer)

	currentRegime := spreadRegime(spreadBPS)
	prevRegime := st.lastRegime
	if st.lastRegime == 0 {
		st.lastRegime = currentRegime
	}
	if st.spreadRing.Len() < r.minSamples {
		return nil
	}

	mean := st.spreadRing.Mean()
	stdDev := st.spreadRing.StdDev()
	z := ZScore(spreadBPS, mean, stdDev)
	transition := prevRegime > 0 && prevRegime != currentRegime
	st.lastRegime = currentRegime

	if z < r.minZScore || spreadBPS <= r.minSpreadBPS || !transition {
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
		EvidenceType: domain.LiquidityEvidenceTypeSpreadRegime,
		TsIngestMs:   event.TsServer,
		Venue:        event.Venue,
		Symbol:       event.Symbol,
		WindowMs:     windowMs,
		Severity:     spreadRegimeSeverity(prevRegime, currentRegime),
		Confidence:   spreadRegimeConfidence(prevRegime, currentRegime),
		Metrics: []domain.LiquidityEvidenceMetric{
			{Key: "mean_bps", Value: mean},
			{Key: "regime", Value: float64(currentRegime)},
			{Key: "spread_bps", Value: spreadBPS},
			{Key: "z_score", Value: z},
		},
		Explain:  []string{fmt.Sprintf("spread regime transition %s -> %s", regimeName(prevRegime), regimeName(currentRegime))},
		Version:  domain.LiquidityEvidenceVersion,
		StreamID: event.StreamID,
		Seq:      event.Seq,
		Watermark: domain.LiquidityInputWatermark{
			SeqStart: st.seqRing.Oldest(),
			SeqEnd:   event.Seq,
		},
	}}
}

func (r *LELSpreadRegimeRule) StreamCount() int { return len(r.streams) }

func (r *LELSpreadRegimeRule) Reset() { r.streams = make(map[string]*lelSpreadRegimeState) }

func (r *LELSpreadRegimeRule) EvictStream(key string) { delete(r.streams, key) }

func (r *LELSpreadRegimeRule) getOrCreate(key string) *lelSpreadRegimeState {
	if st, ok := r.streams[key]; ok {
		return st
	}
	if len(r.streams) >= r.cfg.MaxStreams {
		evictOldest(r.streams)
	}
	st := &lelSpreadRegimeState{}
	r.streams[key] = st
	return st
}

func spreadRegime(spreadBPS float64) int {
	switch {
	case spreadBPS < 5:
		return lelRegimeTight
	case spreadBPS <= 20:
		return lelRegimeNorm
	case spreadBPS <= 50:
		return lelRegimeWide
	default:
		return lelRegimeBlown
	}
}

func regimeName(regime int) string {
	switch regime {
	case lelRegimeTight:
		return "TIGHT"
	case lelRegimeNorm:
		return "NORMAL"
	case lelRegimeWide:
		return "WIDE"
	case lelRegimeBlown:
		return "BLOWN"
	default:
		return "UNKNOWN"
	}
}

func spreadRegimeSeverity(prev, current int) domain.LiquidityEvidenceSeverity {
	switch {
	case current == lelRegimeBlown:
		return domain.LiquidityEvidenceSeverityCritical
	case current == lelRegimeWide:
		return domain.LiquidityEvidenceSeverityHigh
	case prev == lelRegimeNorm && current == lelRegimeWide:
		return domain.LiquidityEvidenceSeverityMedium
	default:
		return domain.LiquidityEvidenceSeverityLow
	}
}

func spreadRegimeConfidence(prev, current int) float64 {
	switch {
	case current == lelRegimeBlown:
		return 0.95
	case current == lelRegimeWide:
		return 0.85
	case prev == lelRegimeNorm && current == lelRegimeWide:
		return 0.70
	default:
		return 0.60
	}
}
