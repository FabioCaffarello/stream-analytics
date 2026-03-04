package app

import (
	"math"

	"github.com/market-raccoon/internal/core/evidence/domain"
)

// imbStreamState holds per-stream state for persistent imbalance detection.
type imbStreamState struct {
	consecutive    int     // count of consecutive |imbalance| > threshold
	lastSign       float64 // sign of last imbalance (+1 or -1)
	streakStartSeq int64
	cooldown       streamEntry
}

// PersistentImbalanceRule detects when the order book depth imbalance
// persists in the same direction for N consecutive observations.
type PersistentImbalanceRule struct {
	cfg            RuleConfig
	threshold      float64
	minConsecutive int
	streams        map[string]*imbStreamState
}

// NewPersistentImbalanceRule creates a persistent imbalance detector.
func NewPersistentImbalanceRule(cfg RuleConfig) *PersistentImbalanceRule {
	return &PersistentImbalanceRule{
		cfg:            cfg,
		threshold:      0.3,
		minConsecutive: 10,
		streams:        make(map[string]*imbStreamState),
	}
}

func (r *PersistentImbalanceRule) Name() string { return string(domain.PersistentImbalance) }

func (r *PersistentImbalanceRule) OnEvent(event domain.RuleEvent) []domain.EvidenceEvent {
	if event.Kind != domain.EventKindBook {
		return nil
	}

	imb := DepthImbalance(event.BidDepth, event.AskDepth)
	absImb := math.Abs(imb)

	key := event.StreamKey()
	st := r.getOrCreate(key)

	if absImb < r.threshold {
		st.consecutive = 0
		st.lastSign = 0
		st.streakStartSeq = 0
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
		st.streakStartSeq = event.Seq
	}
	if st.streakStartSeq <= 0 {
		st.streakStartSeq = event.Seq
	}

	if st.consecutive < r.minConsecutive {
		return nil
	}

	if !st.cooldown.canEmit(event.TsServer, r.cfg.CooldownMs) {
		return nil
	}
	st.cooldown.lastEmitTs = event.TsServer

	sev := imbSeverity(st.consecutive)
	side := "bid"
	if sign < 0 {
		side = "ask"
	}

	return []domain.EvidenceEvent{{
		Type:       domain.PersistentImbalance,
		TsServer:   event.TsServer,
		Venue:      event.Venue,
		Symbol:     event.Symbol,
		StreamID:   resolveStreamID(event),
		Seq:        event.Seq,
		Severity:   sev,
		Confidence: imbConfidence(st.consecutive),
		Features: domain.FeaturesFromMap(map[string]float64{
			"consecutive":   float64(st.consecutive),
			"dominant_side": sign,
			"imbalance":     imb,
		}),
		Explanation: side + "-heavy imbalance persisted over consecutive observations",
		RuleVersion: domain.RuleVersionV0,
		InputWatermark: domain.InputWatermark{
			SeqStart: st.streakStartSeq,
			SeqEnd:   event.Seq,
		},
	}}
}

func (r *PersistentImbalanceRule) StreamCount() int { return len(r.streams) }

func (r *PersistentImbalanceRule) Reset() {
	r.streams = make(map[string]*imbStreamState)
}

func (r *PersistentImbalanceRule) EvictStream(key string) {
	delete(r.streams, key)
}

func (r *PersistentImbalanceRule) getOrCreate(key string) *imbStreamState {
	st, ok := r.streams[key]
	if ok {
		return st
	}
	if len(r.streams) >= r.cfg.MaxStreams {
		evictOldest(r.streams)
	}
	st = &imbStreamState{}
	r.streams[key] = st
	return st
}

func imbSeverity(n int) domain.Severity {
	switch {
	case n >= 30:
		return domain.SeverityCritical
	case n >= 20:
		return domain.SeverityHigh
	default:
		return domain.SeverityMedium
	}
}

func imbConfidence(n int) float64 {
	switch {
	case n >= 30:
		return 0.95
	case n >= 20:
		return 0.85
	default:
		return 0.70
	}
}
