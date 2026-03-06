package app

import (
	"math"
	"strconv"
	"strings"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

type BootstrapConfig struct {
	Source         string
	Boundary       string
	AdapterID      string
	ExecutionMode  string
	EmitFilled     bool
	FillDelayMs    int64
	MaxIntentTTLms int64
	MaxAbsQuantity float64
	MaxNotionalUSD float64
	MaxSlippageBps float64
}

func DefaultBootstrapConfig() BootstrapConfig {
	return BootstrapConfig{
		Source:         "executor.bootstrap.v1",
		Boundary:       "execution.adapter",
		AdapterID:      "bootstrap.simulated",
		ExecutionMode:  "bootstrap_simulated",
		EmitFilled:     true,
		FillDelayMs:    1,
		MaxIntentTTLms: 120_000,
		MaxAbsQuantity: 25,
		MaxNotionalUSD: 25_000,
		MaxSlippageBps: 75,
	}
}

type BootstrapExecutor struct {
	cfg       BootstrapConfig
	streamSeq map[string]int64
}

const (
	executionReasonAccepted               = "accepted_bootstrap_policy"
	executionReasonFilled                 = "filled_synthetic_bootstrap"
	executionReasonIntentIncomplete       = "rejected_intent_incomplete"
	executionReasonSizingNonPositive      = "rejected_sizing_non_positive"
	executionReasonTTLExpired             = "rejected_ttl_expired"
	executionReasonTTLTooLarge            = "rejected_ttl_above_bootstrap_limit"
	executionReasonSizeTooLarge           = "rejected_size_above_bootstrap_limit"
	executionReasonNotionalTooLarge       = "rejected_notional_above_bootstrap_limit"
	executionReasonSlippageTooLarge       = "rejected_slippage_above_bootstrap_limit"
	executionReasonPostOnlyUnsupported    = "rejected_post_only_not_supported"
	executionReasonReduceOnlyUnsupported  = "rejected_reduce_only_not_supported"
	executionReasonIntentValidationFailed = "rejected_intent_validation_failed"
)

type normalizedIntent struct {
	intentID      string
	venue         string
	symbol        string
	accountID     string
	correlationID string
	traceID       string
	requestedQty  float64
	nowMs         int64
	limitPrice    float64
}

func NewBootstrapExecutor(cfg BootstrapConfig) *BootstrapExecutor {
	if strings.TrimSpace(cfg.Source) == "" {
		cfg.Source = "executor.bootstrap.v1"
	}
	if strings.TrimSpace(cfg.Boundary) == "" {
		cfg.Boundary = "execution.adapter"
	}
	if strings.TrimSpace(cfg.AdapterID) == "" {
		cfg.AdapterID = "bootstrap.simulated"
	}
	if strings.TrimSpace(cfg.ExecutionMode) == "" {
		cfg.ExecutionMode = "bootstrap_simulated"
	}
	if cfg.FillDelayMs <= 0 {
		cfg.FillDelayMs = 1
	}
	if cfg.MaxIntentTTLms <= 0 {
		cfg.MaxIntentTTLms = 120_000
	}
	if cfg.MaxAbsQuantity <= 0 {
		cfg.MaxAbsQuantity = 25
	}
	if cfg.MaxNotionalUSD <= 0 {
		cfg.MaxNotionalUSD = 25_000
	}
	if cfg.MaxSlippageBps <= 0 {
		cfg.MaxSlippageBps = 75
	}
	return &BootstrapExecutor{
		cfg:       cfg,
		streamSeq: make(map[string]int64),
	}
}

func (b *BootstrapExecutor) Execute(intent strategydomain.StrategyIntentV1) []executiondomain.ExecutionEventV1 {
	return b.ExecuteAt(intent, 0)
}

func (b *BootstrapExecutor) BoundaryInfo() executionports.BoundaryInfo {
	return executionports.BoundaryInfo{
		Boundary: strings.TrimSpace(b.cfg.Boundary),
		Adapter:  strings.TrimSpace(b.cfg.AdapterID),
		Mode:     strings.TrimSpace(b.cfg.ExecutionMode),
	}
}

// ExecuteAt applies deterministic execution policy using observedAtMs as the
// reference clock for TTL checks. observedAtMs=0 falls back to intent timestamps.
func (b *BootstrapExecutor) ExecuteAt(intent strategydomain.StrategyIntentV1, observedAtMs int64) []executiondomain.ExecutionEventV1 {
	norm := normalizeIntent(intent, observedAtMs)
	streamKey := executionStreamKey(norm.venue, norm.symbol, norm.accountID)
	orderID := sharedhash.HashFieldsFast("bootstrap-order", norm.intentID)

	if reason := b.rejectionReason(intent, norm); reason != "" {
		rejectSeq := b.nextSeq(streamKey)
		return []executiondomain.ExecutionEventV1{{
			EventID:      eventID(norm.intentID, executiondomain.ExecutionStatusRejected, rejectSeq),
			Status:       executiondomain.ExecutionStatusRejected,
			Correlation:  buildCorrelation(norm, orderID),
			TsEventMs:    norm.nowMs,
			TsExchangeMs: 0,
			ExecutionSeq: rejectSeq,
			Attempt:      1,
			RequestedQty: norm.requestedQty,
			LeavesQty:    math.Abs(norm.requestedQty),
			LimitPrice:   norm.limitPrice,
			Reason:       reason,
			Provenance: executiondomain.ExecutionProvenance{
				CorrelationID: norm.correlationID,
				TraceID:       norm.traceID,
				Source:        b.cfg.Source,
			},
		}}
	}

	fillPrice := deterministicFillPrice(intent, norm.requestedQty)
	acceptedSeq := b.nextSeq(streamKey)
	accepted := executiondomain.ExecutionEventV1{
		EventID:      eventID(norm.intentID, executiondomain.ExecutionStatusAccepted, acceptedSeq),
		Status:       executiondomain.ExecutionStatusAccepted,
		Correlation:  buildCorrelation(norm, orderID),
		TsEventMs:    norm.nowMs,
		TsExchangeMs: 0,
		ExecutionSeq: acceptedSeq,
		Attempt:      1,
		RequestedQty: norm.requestedQty,
		LeavesQty:    math.Abs(norm.requestedQty),
		LimitPrice:   norm.limitPrice,
		Reason:       executionReasonAccepted,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: norm.correlationID,
			TraceID:       norm.traceID,
			Source:        b.cfg.Source,
		},
	}

	out := []executiondomain.ExecutionEventV1{accepted}
	if !b.cfg.EmitFilled {
		return out
	}

	filledSeq := b.nextSeq(streamKey)
	filled := executiondomain.ExecutionEventV1{
		EventID:      eventID(norm.intentID, executiondomain.ExecutionStatusFilled, filledSeq),
		Status:       executiondomain.ExecutionStatusFilled,
		Correlation:  buildCorrelation(norm, orderID),
		TsEventMs:    norm.nowMs + b.cfg.FillDelayMs,
		TsExchangeMs: norm.nowMs + b.cfg.FillDelayMs,
		ExecutionSeq: filledSeq,
		Attempt:      1,
		RequestedQty: norm.requestedQty,
		// Bootstrap convention: quantities carry intent side sign.
		CumulativeFilledQty: norm.requestedQty,
		LastFillQty:         norm.requestedQty,
		LeavesQty:           0,
		LimitPrice:          norm.limitPrice,
		AvgFillPrice:        fillPrice,
		LastFillPrice:       fillPrice,
		Reason:              executionReasonFilled,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: norm.correlationID,
			TraceID:       norm.traceID,
			Source:        b.cfg.Source,
		},
	}

	out = append(out, filled)
	return out
}

func (b *BootstrapExecutor) nextSeq(streamKey string) int64 {
	next := b.streamSeq[streamKey] + 1
	b.streamSeq[streamKey] = next
	return next
}

func executionStreamKey(venue, symbol, accountID string) string {
	return strings.ToLower(strings.TrimSpace(venue)) + "|" + strings.ToUpper(strings.TrimSpace(symbol)) + "|" + strings.TrimSpace(accountID)
}

func buildCorrelation(intent normalizedIntent, orderID string) executiondomain.ExecutionCorrelation {
	venueOrderID := "SIM-" + strings.ToUpper(orderID)
	if len(venueOrderID) > 20 {
		venueOrderID = venueOrderID[:20]
	}
	return executiondomain.ExecutionCorrelation{
		IntentID:      intent.intentID,
		OrderID:       orderID,
		VenueOrderID:  venueOrderID,
		ClientOrderID: "CID-" + strconv.FormatInt(intent.nowMs, 10),
		Venue:         intent.venue,
		Symbol:        intent.symbol,
		AccountID:     intent.accountID,
	}
}

func deterministicFillPrice(intent strategydomain.StrategyIntentV1, requestedQty float64) float64 {
	if intent.Constraints.OrderType == strategydomain.OrderTypeLimit && intent.Constraints.LimitPrice > 0 {
		return intent.Constraints.LimitPrice
	}
	absQty := math.Abs(requestedQty)
	if absQty > 0 && intent.Sizing.MaxNotionalUSD > 0 {
		return intent.Sizing.MaxNotionalUSD / absQty
	}
	return 100
}

func eventID(intentID string, status executiondomain.ExecutionStatus, seq int64) string {
	return sharedhash.HashFieldsFast("execution-event-v1", intentID, string(status), strconv.FormatInt(seq, 10))
}

func normalizeIntent(intent strategydomain.StrategyIntentV1, observedAtMs int64) normalizedIntent {
	venue := strings.ToLower(strings.TrimSpace(intent.Scope.Venue))
	if venue == "" {
		venue = "unknown"
	}
	symbol := strings.ToUpper(strings.TrimSpace(intent.Scope.Symbol))
	if symbol == "" {
		symbol = "UNKNOWN"
	}
	accountID := strings.TrimSpace(intent.Scope.AccountID)
	if accountID == "" {
		accountID = "paper"
	}
	intentID := strings.TrimSpace(intent.IntentID)
	if intentID == "" {
		intentID = sharedhash.HashFieldsFast(
			"bootstrap-intent-fallback",
			venue,
			symbol,
			strconv.FormatInt(intent.CreatedAtMs, 10),
			strconv.FormatInt(intent.ExpiresAtMs, 10),
		)
	}
	nowMs := observedAtMs
	if nowMs <= 0 {
		nowMs = intent.CreatedAtMs
	}
	if nowMs <= 0 {
		nowMs = intent.ExpiresAtMs
	}
	if nowMs <= 0 {
		nowMs = 1
	}
	correlationID := strings.TrimSpace(intent.Provenance.CorrelationID)
	if correlationID == "" {
		correlationID = sharedhash.HashFieldsFast("bootstrap-correlation", intentID)
	}
	requestedQty := normalizedRequestedQty(intent.Side, intent.Sizing.Value)
	limitPrice := intent.Constraints.LimitPrice
	if !finiteNonNegative(limitPrice) {
		limitPrice = 0
	}

	return normalizedIntent{
		intentID:      intentID,
		venue:         venue,
		symbol:        symbol,
		accountID:     accountID,
		correlationID: correlationID,
		traceID:       strings.TrimSpace(intent.Provenance.TraceID),
		requestedQty:  requestedQty,
		nowMs:         nowMs,
		limitPrice:    limitPrice,
	}
}

//nolint:gocyclo // explicit deterministic policy order is intentional.
func (b *BootstrapExecutor) rejectionReason(intent strategydomain.StrategyIntentV1, norm normalizedIntent) string {
	if strings.TrimSpace(intent.IntentID) == "" ||
		strings.TrimSpace(intent.Scope.Venue) == "" ||
		strings.TrimSpace(intent.Scope.Symbol) == "" {
		return executionReasonIntentIncomplete
	}
	if !finite(intent.Sizing.Value) || intent.Sizing.Value <= 0 {
		return executionReasonSizingNonPositive
	}
	if intent.ExpiresAtMs <= norm.nowMs {
		return executionReasonTTLExpired
	}
	if intent.ExpiresAtMs-intent.CreatedAtMs > b.cfg.MaxIntentTTLms {
		return executionReasonTTLTooLarge
	}
	if math.Abs(intent.Sizing.Value) > b.cfg.MaxAbsQuantity {
		return executionReasonSizeTooLarge
	}
	if intent.Sizing.MaxNotionalUSD > b.cfg.MaxNotionalUSD {
		return executionReasonNotionalTooLarge
	}
	if intent.Constraints.MaxSlippageBps > b.cfg.MaxSlippageBps {
		return executionReasonSlippageTooLarge
	}
	if intent.Constraints.PostOnly {
		return executionReasonPostOnlyUnsupported
	}
	if intent.Constraints.ReduceOnly {
		return executionReasonReduceOnlyUnsupported
	}
	if p := intent.Validate(); p != nil {
		return executionReasonIntentValidationFailed
	}
	return ""
}

func normalizedRequestedQty(side strategydomain.IntentSide, qty float64) float64 {
	if !finite(qty) {
		return 0
	}
	if qty < 0 {
		qty = -qty
	}
	if side == strategydomain.IntentSideSell {
		return -qty
	}
	return qty
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func finiteNonNegative(v float64) bool {
	return finite(v) && v >= 0
}
