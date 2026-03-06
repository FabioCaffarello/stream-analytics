package app

import (
	"strconv"
	"strings"

	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

type PlannerConfig struct {
	StrategyID         string
	StrategyVersion    string
	StrategyInstanceID string
	AccountID          string
	IntentTTLms        int64
	BaseQuantity       float64
	MaxNotionalUSD     float64
	MaxSlippageBps     float64
}

func DefaultPlannerConfig() PlannerConfig {
	return PlannerConfig{
		StrategyID:         "bootstrap-strategy",
		StrategyVersion:    "v0",
		StrategyInstanceID: "strategist-bootstrap",
		AccountID:          "paper",
		IntentTTLms:        30_000,
		BaseQuantity:       1,
		MaxNotionalUSD:     2_500,
		MaxSlippageBps:     25,
	}
}

type IntentInput struct {
	Kind          string
	Venue         string
	Instrument    string
	SignalID      string
	CorrelationID string
	TraceID       string
	Reason        string
	Confidence    float64
	TsServer      int64
	Seq           int64
}

type IntentPlanner struct {
	cfg PlannerConfig
}

func NewIntentPlanner(cfg PlannerConfig) *IntentPlanner {
	if strings.TrimSpace(cfg.StrategyID) == "" {
		cfg.StrategyID = "bootstrap-strategy"
	}
	if strings.TrimSpace(cfg.StrategyVersion) == "" {
		cfg.StrategyVersion = "v0"
	}
	if strings.TrimSpace(cfg.StrategyInstanceID) == "" {
		cfg.StrategyInstanceID = "strategist-bootstrap"
	}
	if strings.TrimSpace(cfg.AccountID) == "" {
		cfg.AccountID = "paper"
	}
	if cfg.IntentTTLms <= 0 {
		cfg.IntentTTLms = 30_000
	}
	if cfg.BaseQuantity <= 0 {
		cfg.BaseQuantity = 1
	}
	if cfg.MaxNotionalUSD <= 0 {
		cfg.MaxNotionalUSD = 2_500
	}
	if cfg.MaxSlippageBps <= 0 {
		cfg.MaxSlippageBps = 25
	}
	return &IntentPlanner{cfg: cfg}
}

func (p *IntentPlanner) Plan(input IntentInput) (strategydomain.StrategyIntentV1, bool) {
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	venue := strings.ToLower(strings.TrimSpace(input.Venue))
	instrument := normalizeInstrument(input.Instrument)
	if kind == "" || venue == "" || instrument == "" || input.TsServer <= 0 {
		return strategydomain.StrategyIntentV1{}, false
	}

	signalID := strings.TrimSpace(input.SignalID)
	if signalID == "" {
		signalID = sharedhash.HashFieldsFast(
			"signal-fallback",
			kind,
			venue,
			instrument,
			strconv.FormatInt(input.Seq, 10),
		)
	}

	side := classifySide(kind)
	sizingValue := p.cfg.BaseQuantity * (0.5 + clampConfidence(input.Confidence))
	intentID := sharedhash.HashFieldsFast(
		"strategy-intent-v1",
		kind,
		venue,
		instrument,
		signalID,
		strconv.FormatInt(input.Seq, 10),
	)

	correlationID := strings.TrimSpace(input.CorrelationID)
	if correlationID == "" {
		correlationID = sharedhash.HashFieldsFast("corr", intentID)
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "bootstrap intent from canonical signal"
	}

	intent := strategydomain.StrategyIntentV1{
		IntentID: intentID,
		Strategy: strategydomain.StrategyRef{
			StrategyID:         p.cfg.StrategyID,
			StrategyVersion:    p.cfg.StrategyVersion,
			StrategyInstanceID: p.cfg.StrategyInstanceID,
		},
		Scope: strategydomain.IntentScope{
			Venue:     venue,
			Symbol:    instrument,
			AccountID: p.cfg.AccountID,
		},
		Side: side,
		Sizing: strategydomain.SizingIntent{
			Mode:           strategydomain.SizingModeBaseQuantity,
			Value:          sizingValue,
			MaxNotionalUSD: p.cfg.MaxNotionalUSD,
		},
		Constraints: strategydomain.ExecutionConstraints{
			OrderType:      strategydomain.OrderTypeMarket,
			TimeInForce:    strategydomain.TimeInForceIOC,
			LimitPrice:     0,
			MaxSlippageBps: p.cfg.MaxSlippageBps,
			PostOnly:       false,
			ReduceOnly:     false,
		},
		CreatedAtMs: input.TsServer,
		ExpiresAtMs: input.TsServer + p.cfg.IntentTTLms,
		Provenance: strategydomain.IntentProvenance{
			Reason:          reason,
			CorrelationID:   correlationID,
			TraceID:         strings.TrimSpace(input.TraceID),
			ParentSignalIDs: []string{signalID},
			PolicyHash: sharedhash.HashFieldsFast(
				"strategy-bootstrap-policy-v1",
				p.cfg.StrategyID,
				p.cfg.StrategyVersion,
				p.cfg.StrategyInstanceID,
			),
		},
	}

	if p := intent.Validate(); p != nil {
		return strategydomain.StrategyIntentV1{}, false
	}
	return intent, true
}

func classifySide(kind string) strategydomain.IntentSide {
	if strings.Contains(kind, "sell") ||
		strings.Contains(kind, "thinning") ||
		strings.Contains(kind, "explosion") ||
		strings.Contains(kind, "collapse") ||
		strings.Contains(kind, "down") {
		return strategydomain.IntentSideSell
	}
	return strategydomain.IntentSideBuy
}

func clampConfidence(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizeInstrument(v string) string {
	return strings.ToUpper(strings.TrimSpace(v))
}
