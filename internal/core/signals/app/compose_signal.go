package app

import (
	"strconv"
	"strings"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	signalsdomain "github.com/market-raccoon/internal/core/signals/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

// ComposePolicy defines deterministic composition constraints for rules 1-3.
type ComposePolicy struct {
	CorrelationWindowMs int64
	CorrelationCap      int
	RegimeBoostFactor   float64
	CrossVenueBoost     float64
}

// DefaultComposePolicy returns production defaults.
func DefaultComposePolicy() ComposePolicy {
	return ComposePolicy{
		CorrelationWindowMs: 5000,
		CorrelationCap:      100,
		RegimeBoostFactor:   0.20,
		CrossVenueBoost:     1.15,
	}
}

// ComposeInput is one composition request from a microstructure event.
type ComposeInput struct {
	Micro     evidencedomain.EvidenceEvent
	Regime    *evidencedomain.RegimeSignal
	Timeframe string
}

// ComposeResult reports composition output and rule hit flags.
type ComposeResult struct {
	Signal            signalsdomain.CompositeSignalV1
	RegimeBoosted     bool
	CorrelationHit    bool
	CorrelationSpanMs int64
}

type microObservation struct {
	Kind       string
	Venue      string
	Instrument string
	TsServer   int64
	Confidence float64
}

// SignalComposer applies rules 1-3 with bounded correlation history.
type SignalComposer struct {
	policy  ComposePolicy
	history []microObservation
}

// NewSignalComposer creates a deterministic signal composer.
func NewSignalComposer(policy ComposePolicy) *SignalComposer {
	if policy.CorrelationWindowMs <= 0 {
		policy.CorrelationWindowMs = 5000
	}
	if policy.CorrelationCap <= 0 {
		policy.CorrelationCap = 100
	}
	if policy.RegimeBoostFactor <= 0 {
		policy.RegimeBoostFactor = 0.20
	}
	if policy.CrossVenueBoost <= 1 {
		policy.CrossVenueBoost = 1.15
	}
	return &SignalComposer{
		policy:  policy,
		history: make([]microObservation, 0, policy.CorrelationCap),
	}
}

// Compose applies rules 1-3 and returns a composed signal when eligible.
//
//nolint:gocyclo // Rule composition keeps explicit deterministic branching for auditability.
func (c *SignalComposer) Compose(input ComposeInput) (ComposeResult, bool) {
	micro := input.Micro
	if p := micro.Validate(); p != nil {
		return ComposeResult{}, false
	}

	timeframe := normalizedTimeframe(input.Timeframe)
	if input.Regime != nil {
		if tf := strings.TrimSpace(input.Regime.Timeframe); tf != "" {
			timeframe = tf
		}
	}

	obs := microObservation{
		Kind:       string(micro.Type),
		Venue:      micro.Venue,
		Instrument: micro.Symbol,
		TsServer:   micro.TsServer,
		Confidence: micro.Confidence,
	}
	correlated := c.correlatedWindow(obs)
	c.pushObservation(obs)

	rule1 := micro.Confidence > 0.7 && severityAtLeast(micro.Severity, evidencedomain.SeverityMedium)

	confidence := micro.Confidence
	regimeBoosted := false
	regimeKind := ""
	regimeStrength := 0.0
	if input.Regime != nil {
		regimeKind = string(input.Regime.Kind)
		regimeStrength = input.Regime.Strength
		if micro.Confidence > 0.5 && regimeStrength > 0.6 && regimeMatches(micro.Type, input.Regime.Kind) {
			confidence = micro.Confidence * (1 + c.policy.RegimeBoostFactor*regimeStrength)
			regimeBoosted = true
		}
	}
	confidence = capConfidence(confidence)

	correlationHit := len(correlated.venues) >= 2
	if correlationHit {
		crossConfidence := capConfidence(correlated.maxConfidence * c.policy.CrossVenueBoost)
		if crossConfidence > confidence {
			confidence = crossConfidence
		}
	}

	if !rule1 && !regimeBoosted && !correlationHit {
		return ComposeResult{}, false
	}

	sourceKinds := []string{string(micro.Type)}
	if regimeBoosted {
		sourceKinds = append(sourceKinds, regimeKind)
	}
	if correlationHit {
		sourceKinds = append(sourceKinds, "cross_venue_confirmation")
	}

	signal := signalsdomain.CompositeSignalV1{
		Kind:           string(micro.Type),
		Venue:          micro.Venue,
		Instrument:     micro.Symbol,
		Timeframe:      timeframe,
		TsServer:       micro.TsServer,
		Severity:       string(micro.Severity),
		Confidence:     confidence,
		Evidence:       buildSignalFeatures(micro),
		RegimeKind:     regimeKind,
		RegimeStrength: regimeStrength,
		Reason:         buildReason(micro.Explanation, regimeBoosted, correlationHit),
		Seq:            micro.Seq,
		SourceKinds:    sourceKinds,
	}
	signal.SignalID = compositeSignalID(signal)
	signal.CorrelationID = compositeCorrelationID(signal, micro)
	if p := signal.Validate(); p != nil {
		return ComposeResult{}, false
	}
	return ComposeResult{
		Signal:            signal,
		RegimeBoosted:     regimeBoosted,
		CorrelationHit:    correlationHit,
		CorrelationSpanMs: correlated.spanMs,
	}, true
}

type correlationSummary struct {
	venues        map[string]struct{}
	maxConfidence float64
	spanMs        int64
}

func (c *SignalComposer) correlatedWindow(current microObservation) correlationSummary {
	summary := correlationSummary{
		venues:        map[string]struct{}{current.Venue: {}},
		maxConfidence: current.Confidence,
		spanMs:        0,
	}
	minTs := current.TsServer
	maxTs := current.TsServer
	for i := len(c.history) - 1; i >= 0; i-- {
		obs := c.history[i]
		if obs.Kind != current.Kind || obs.Instrument != current.Instrument {
			continue
		}
		delta := current.TsServer - obs.TsServer
		if delta < 0 {
			delta = -delta
		}
		if delta > c.policy.CorrelationWindowMs {
			continue
		}
		summary.venues[obs.Venue] = struct{}{}
		if obs.Confidence > summary.maxConfidence {
			summary.maxConfidence = obs.Confidence
		}
		if obs.TsServer < minTs {
			minTs = obs.TsServer
		}
		if obs.TsServer > maxTs {
			maxTs = obs.TsServer
		}
	}
	summary.spanMs = maxTs - minTs
	return summary
}

func (c *SignalComposer) pushObservation(obs microObservation) {
	if len(c.history) < c.policy.CorrelationCap {
		c.history = append(c.history, obs)
		return
	}
	copy(c.history, c.history[1:])
	c.history[len(c.history)-1] = obs
}

func normalizedTimeframe(v string) string {
	if tf := strings.TrimSpace(v); tf != "" {
		return tf
	}
	return "raw"
}

func severityAtLeast(got, floor evidencedomain.Severity) bool {
	return severityRank(got) >= severityRank(floor)
}

func severityRank(sev evidencedomain.Severity) int {
	switch sev {
	case evidencedomain.SeverityCritical:
		return 4
	case evidencedomain.SeverityHigh:
		return 3
	case evidencedomain.SeverityMedium:
		return 2
	default:
		return 1
	}
}

func regimeMatches(kind evidencedomain.EvidenceType, regime evidencedomain.RegimeKind) bool {
	switch kind {
	case evidencedomain.Absorption, evidencedomain.PersistentImbalance:
		return regime == evidencedomain.RegimeTrending
	case evidencedomain.Sweep:
		return regime == evidencedomain.RegimeBreakout || regime == evidencedomain.RegimeHighVolatility
	case evidencedomain.SpreadExplosion, evidencedomain.LiquidityThinning:
		return regime == evidencedomain.RegimeHighVolatility || regime == evidencedomain.RegimeBreakout
	default:
		return false
	}
}

func capConfidence(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 0.99 {
		return 0.99
	}
	return v
}

func buildSignalFeatures(micro evidencedomain.EvidenceEvent) []signalsdomain.SignalFeature {
	if len(micro.Features) == 0 {
		return []signalsdomain.SignalFeature{{Label: "evidence", Value: "missing"}}
	}
	features := make([]signalsdomain.SignalFeature, 0, len(micro.Features))
	for i := range micro.Features {
		label := strings.TrimSpace(micro.Features[i].Key)
		if label == "" {
			continue
		}
		features = append(features, signalsdomain.SignalFeature{
			Label: label,
			Value: strconv.FormatFloat(micro.Features[i].Value, 'f', 6, 64),
		})
	}
	if len(features) == 0 {
		return []signalsdomain.SignalFeature{{Label: "evidence", Value: "missing"}}
	}
	return features
}

func buildReason(base string, regimeBoosted, correlationHit bool) string {
	reason := strings.TrimSpace(base)
	if reason == "" {
		reason = "composed signal"
	}
	if regimeBoosted {
		reason += " | regime_boost"
	}
	if correlationHit {
		reason += " | cross_venue_confirmed"
	}
	return reason
}

func compositeSignalID(s signalsdomain.CompositeSignalV1) string {
	return "csig_" + sharedhash.HashFieldsFast(
		s.Kind,
		s.Venue,
		s.Instrument,
		s.Timeframe,
		strconv.FormatInt(s.TsServer, 10),
		strconv.FormatInt(s.Seq, 10),
		s.Severity,
		strconv.FormatFloat(s.Confidence, 'f', 6, 64),
	)
}

func compositeCorrelationID(s signalsdomain.CompositeSignalV1, micro evidencedomain.EvidenceEvent) string {
	return sharedhash.HashFieldsFast(
		"composite",
		s.Kind,
		s.Venue,
		s.Instrument,
		strconv.FormatInt(s.TsServer, 10),
		strconv.FormatInt(micro.Seq, 10),
		micro.StreamID,
	)
}
