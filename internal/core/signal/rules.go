package signal

import (
	"math"
	"sort"
	"strings"

	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	marketmodel "github.com/market-raccoon/internal/core/marketmodel"
)

type SignalRule interface {
	Name() string
	Evaluate(input RuleInput) (RuleOutput, bool)
}

type RuleInput struct {
	Tenant    string
	StreamKey marketmodel.StreamKey
	Evidence  evidencedomain.EvidenceEvent
	Snapshot  StreamSnapshot
}

type RuleOutput struct {
	Type        string
	Scope       marketmodel.SignalScope
	Severity    string
	Confidence  float64
	Features    []marketmodel.SignalFeature
	Explanation string
	RuleVersion string
}

type RulesConfig struct {
	RegimeChange        RegimeChangeConfig
	LiquidityCollapse   LiquidityCollapseConfig
	PersistentImbalance PersistentImbalanceConfig
	VenueDivergence     VenueDivergenceConfig
	DefaultRuleVersion  string
}

func DefaultRulesConfig() RulesConfig {
	return RulesConfig{
		RegimeChange: RegimeChangeConfig{
			WindowMs:         3000,
			MinBurst:         3,
			MinDistinctTypes: 2,
			RuleVersion:      "v0",
		},
		LiquidityCollapse: LiquidityCollapseConfig{
			WindowMs:          5000,
			MinSpreadEvents:   1,
			MinThinningEvents: 1,
			RuleVersion:       "v0",
		},
		PersistentImbalance: PersistentImbalanceConfig{
			WindowMs:             5000,
			MinImbalanceEvents:   2,
			RequireAbsorptionHit: true,
			RuleVersion:          "v0",
		},
		VenueDivergence: VenueDivergenceConfig{
			Enabled:       false,
			RuleVersion:   "v0",
			AggregatorCap: 0,
		},
		DefaultRuleVersion: "v0",
	}
}

func BuildV0Rules(cfg RulesConfig) []SignalRule {
	return []SignalRule{
		RegimeChangeRule{cfg: cfg.RegimeChange},
		LiquidityCollapseRule{cfg: cfg.LiquidityCollapse},
		PersistentImbalanceRule{cfg: cfg.PersistentImbalance},
		VenueDivergenceRule{cfg: cfg.VenueDivergence},
	}
}

type RegimeChangeConfig struct {
	WindowMs         int64
	MinBurst         int
	MinDistinctTypes int
	RuleVersion      string
}

type RegimeChangeRule struct{ cfg RegimeChangeConfig }

func (r RegimeChangeRule) Name() string { return "RegimeChange" }

func (r RegimeChangeRule) Evaluate(input RuleInput) (RuleOutput, bool) {
	if r.cfg.WindowMs <= 0 {
		return RuleOutput{}, false
	}
	window := evidenceWindow(input.Snapshot.EvidenceHistory, input.Evidence.TsServer, r.cfg.WindowMs)
	if len(window) < r.cfg.MinBurst {
		return RuleOutput{}, false
	}
	seen := make([]string, 0, len(window))
	confidenceSum := 0.0
	for i := range window {
		confidenceSum += window[i].Confidence
		kind := string(window[i].Type)
		if indexOfString(seen, kind) < 0 {
			seen = append(seen, kind)
		}
	}
	sort.Strings(seen)
	if len(seen) < r.cfg.MinDistinctTypes {
		return RuleOutput{}, false
	}
	avgConfidence := confidenceSum / float64(len(window))
	severity := "medium"
	if len(window) >= r.cfg.MinBurst+2 {
		severity = "high"
	}
	if len(window) >= r.cfg.MinBurst+4 {
		severity = "critical"
	}
	return RuleOutput{
		Type:       "regime_change",
		Scope:      marketmodel.SignalScopeStream,
		Severity:   severity,
		Confidence: clamp01(avgConfidence + 0.10),
		Features: []marketmodel.SignalFeature{
			{Key: "burst_count", Value: float64(len(window))},
			{Key: "distinct_evidence_types", Value: float64(len(seen))},
			{Key: "mean_confidence", Value: avgConfidence},
		},
		Explanation: "evidence burst indicates regime transition pressure",
		RuleVersion: fallbackRuleVersion(r.cfg.RuleVersion),
	}, true
}

type LiquidityCollapseConfig struct {
	WindowMs          int64
	MinSpreadEvents   int
	MinThinningEvents int
	RuleVersion       string
}

type LiquidityCollapseRule struct{ cfg LiquidityCollapseConfig }

func (r LiquidityCollapseRule) Name() string { return "LiquidityCollapse" }

func (r LiquidityCollapseRule) Evaluate(input RuleInput) (RuleOutput, bool) {
	if r.cfg.WindowMs <= 0 {
		return RuleOutput{}, false
	}
	window := evidenceWindow(input.Snapshot.EvidenceHistory, input.Evidence.TsServer, r.cfg.WindowMs)
	spreadCount := 0
	thinningCount := 0
	for i := range window {
		switch window[i].Type {
		case evidencedomain.SpreadExplosion:
			spreadCount++
		case evidencedomain.LiquidityThinning:
			thinningCount++
		}
	}
	if spreadCount < r.cfg.MinSpreadEvents || thinningCount < r.cfg.MinThinningEvents {
		return RuleOutput{}, false
	}
	severity := "high"
	if spreadCount >= r.cfg.MinSpreadEvents+1 && thinningCount >= r.cfg.MinThinningEvents+1 {
		severity = "critical"
	}
	confidence := 0.75 + 0.05*float64(spreadCount+thinningCount-r.cfg.MinSpreadEvents-r.cfg.MinThinningEvents)
	return RuleOutput{
		Type:       "liquidity_collapse",
		Scope:      marketmodel.SignalScopeStream,
		Severity:   severity,
		Confidence: clamp01(confidence),
		Features: []marketmodel.SignalFeature{
			{Key: "spread_explosion_events", Value: float64(spreadCount)},
			{Key: "thinning_events", Value: float64(thinningCount)},
			{Key: "window_ms", Value: float64(r.cfg.WindowMs)},
		},
		Explanation: "thinning and spread explosion co-occurred in the same window",
		RuleVersion: fallbackRuleVersion(r.cfg.RuleVersion),
	}, true
}

type PersistentImbalanceConfig struct {
	WindowMs             int64
	MinImbalanceEvents   int
	RequireAbsorptionHit bool
	RuleVersion          string
}

type PersistentImbalanceRule struct{ cfg PersistentImbalanceConfig }

func (r PersistentImbalanceRule) Name() string { return "PersistentImbalanceSignal" }

func (r PersistentImbalanceRule) Evaluate(input RuleInput) (RuleOutput, bool) {
	if r.cfg.WindowMs <= 0 {
		return RuleOutput{}, false
	}
	window := evidenceWindow(input.Snapshot.EvidenceHistory, input.Evidence.TsServer, r.cfg.WindowMs)
	imbalance := 0
	absorption := 0
	for i := range window {
		switch window[i].Type {
		case evidencedomain.PersistentImbalance:
			imbalance++
		case evidencedomain.Absorption:
			absorption++
		}
	}
	if imbalance < r.cfg.MinImbalanceEvents {
		return RuleOutput{}, false
	}
	if r.cfg.RequireAbsorptionHit && absorption == 0 {
		return RuleOutput{}, false
	}
	severity := "medium"
	if imbalance >= r.cfg.MinImbalanceEvents+1 {
		severity = "high"
	}
	if absorption >= 2 {
		severity = "critical"
	}
	return RuleOutput{
		Type:       "persistent_imbalance_signal",
		Scope:      marketmodel.SignalScopeStream,
		Severity:   severity,
		Confidence: clamp01(0.70 + 0.05*float64(imbalance) + 0.10*float64(absorption)),
		Features: []marketmodel.SignalFeature{
			{Key: "absorption_events", Value: float64(absorption)},
			{Key: "imbalance_events", Value: float64(imbalance)},
			{Key: "window_ms", Value: float64(r.cfg.WindowMs)},
		},
		Explanation: "persistent imbalance persisted with absorption evidence",
		RuleVersion: fallbackRuleVersion(r.cfg.RuleVersion),
	}, true
}

type VenueDivergenceConfig struct {
	Enabled       bool
	RuleVersion   string
	AggregatorCap int
}

type VenueDivergenceRule struct{ cfg VenueDivergenceConfig }

func (r VenueDivergenceRule) Name() string { return "VenueDivergenceSignal" }

func (r VenueDivergenceRule) Evaluate(input RuleInput) (RuleOutput, bool) {
	// Deterministic stub: emit only when explicitly enabled and
	// multi-venue aggregator capability is confirmed.
	if !r.cfg.Enabled || r.cfg.AggregatorCap <= 0 {
		return RuleOutput{}, false
	}
	return RuleOutput{
		Type:       "venue_divergence_signal",
		Scope:      marketmodel.SignalScopeMarket,
		Severity:   "medium",
		Confidence: 0.60,
		Features: []marketmodel.SignalFeature{
			{Key: "aggregator_cap", Value: float64(r.cfg.AggregatorCap)},
		},
		Explanation: "multi-venue divergence detector enabled",
		RuleVersion: fallbackRuleVersion(r.cfg.RuleVersion),
	}, true
}

func evidenceWindow(history []evidencedomain.EvidenceEvent, tsServer, windowMs int64) []evidencedomain.EvidenceEvent {
	if len(history) == 0 {
		return nil
	}
	out := make([]evidencedomain.EvidenceEvent, 0, len(history))
	for i := range history {
		delta := tsServer - history[i].TsServer
		if delta < 0 {
			delta = -delta
		}
		if delta <= windowMs {
			out = append(out, history[i])
		}
	}
	return out
}

func indexOfString(in []string, target string) int {
	for i := range in {
		if in[i] == target {
			return i
		}
	}
	return -1
}

func fallbackRuleVersion(v string) string {
	if strings.TrimSpace(v) == "" {
		return "v0"
	}
	return strings.TrimSpace(v)
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
