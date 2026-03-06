package binance

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	executioncred "github.com/market-raccoon/internal/adapters/execution/credentials"
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	executionports "github.com/market-raccoon/internal/core/execution/ports"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

const (
	reasonAcceptedRealTestOrder           = "accepted_real_adapter_test_order"
	reasonAcceptedRealLifecycle           = "accepted_real_adapter_safe_lifecycle"
	reasonPlacedObserved                  = "placed_real_adapter_observed"
	reasonPartiallyFilledObserved         = "partially_filled_real_adapter_observed"
	reasonFilledObserved                  = "filled_real_adapter_observed"
	reasonCanceledObserved                = "canceled_real_adapter_observed"
	reasonExpiredObserved                 = "expired_real_adapter_observed"
	reasonFailedObserved                  = "failed_real_adapter_observed"
	reasonFailedUnknownVenueStatus        = "failed_unknown_venue_status"
	reasonFailedReconciliationPoll        = "failed_reconciliation_poll"
	reasonFailedReconciliationTimeout     = "failed_reconciliation_timeout"
	reasonRejectedIntentIncomplete        = "rejected_intent_incomplete"
	reasonRejectedIntentValidationFailed  = "rejected_intent_validation_failed"
	reasonRejectedSizingModeUnsupported   = "rejected_sizing_mode_not_supported"
	reasonRejectedPostOnlyUnsupported     = "rejected_post_only_not_supported"
	reasonRejectedReduceOnlyUnsupported   = "rejected_reduce_only_not_supported"
	reasonRejectedOrderTypeUnsupported    = "rejected_order_type_not_supported"
	reasonRejectedTimeInForceUnsupported  = "rejected_time_in_force_not_supported"
	reasonRejectedEndpointModeUnsupported = "rejected_endpoint_mode_not_supported"
	reasonRejectedReconciliationRequired  = "rejected_reconciliation_required"
)

const (
	endpointModeTestOrder     = "test_order"
	endpointModeSafeLifecycle = "safe_order_lifecycle"
	quantityEpsilon           = 1e-9
)

type tradeGateway interface {
	SubmitTestOrder(ctx context.Context, req TestOrderRequest) (string, error)
	SubmitOrder(ctx context.Context, req TestOrderRequest) (OrderSnapshot, error)
	QueryOrder(ctx context.Context, symbol, venueOrderID, clientOrderID string, timestampMs int64) (OrderSnapshot, error)
}

// SafeIntentExecutorConfig defines Stage 7 real-adapter safe-mode guardrails.
type SafeIntentExecutorConfig struct {
	Source             string
	Boundary           string
	AdapterID          string
	ExecutionMode      string
	EndpointMode       string
	ReconcileEnabled   bool
	ReconcilePollEvery time.Duration
	ReconcileMaxPolls  int
}

// SafeIntentExecutor implements executionports.IntentExecutor via Binance test-order API.
type SafeIntentExecutor struct {
	cfg       SafeIntentExecutorConfig
	gateway   tradeGateway
	streamSeq map[string]int64
}

func DefaultSafeIntentExecutorConfig() SafeIntentExecutorConfig {
	return SafeIntentExecutorConfig{
		Source:             "executor.real_adapter.safe.v1",
		Boundary:           "execution.adapter",
		AdapterID:          "binance.spot",
		ExecutionMode:      "real_adapter_safe",
		EndpointMode:       endpointModeTestOrder,
		ReconcileEnabled:   false,
		ReconcilePollEvery: 500 * time.Millisecond,
		ReconcileMaxPolls:  6,
	}
}

func NewSafeIntentExecutor(cfg SafeIntentExecutorConfig, gateway tradeGateway) *SafeIntentExecutor {
	if strings.TrimSpace(cfg.Source) == "" {
		cfg.Source = "executor.real_adapter.safe.v1"
	}
	if strings.TrimSpace(cfg.Boundary) == "" {
		cfg.Boundary = "execution.adapter"
	}
	if strings.TrimSpace(cfg.AdapterID) == "" {
		cfg.AdapterID = "binance.spot"
	}
	if strings.TrimSpace(cfg.ExecutionMode) == "" {
		cfg.ExecutionMode = "real_adapter_safe"
	}
	if strings.TrimSpace(cfg.EndpointMode) == "" {
		cfg.EndpointMode = endpointModeTestOrder
	}
	if cfg.ReconcilePollEvery <= 0 {
		cfg.ReconcilePollEvery = 500 * time.Millisecond
	}
	if cfg.ReconcileMaxPolls <= 0 {
		cfg.ReconcileMaxPolls = 6
	}
	return &SafeIntentExecutor{
		cfg:       cfg,
		gateway:   gateway,
		streamSeq: make(map[string]int64),
	}
}

func (e *SafeIntentExecutor) BoundaryInfo() executionports.BoundaryInfo {
	return executionports.BoundaryInfo{
		Boundary: strings.TrimSpace(e.cfg.Boundary),
		Adapter:  strings.TrimSpace(e.cfg.AdapterID),
		Mode:     strings.TrimSpace(e.cfg.ExecutionMode),
	}
}

//nolint:gocyclo // Execution boundary keeps rejection, test-order, and lifecycle paths explicit for auditability.
func (e *SafeIntentExecutor) ExecuteAt(intent strategydomain.StrategyIntentV1, observedAtMs int64) []executiondomain.ExecutionEventV1 {
	norm := normalizeIntent(intent, observedAtMs)
	streamKey := executionStreamKey(norm.venue, norm.symbol, norm.accountID)
	orderID := sharedhash.HashFieldsFast("real-adapter-order", norm.intentID)

	if reason := e.rejectionReason(intent); reason != "" {
		return []executiondomain.ExecutionEventV1{e.rejectEvent(streamKey, norm, orderID, reason)}
	}
	if e.gateway == nil {
		return []executiondomain.ExecutionEventV1{e.rejectEvent(streamKey, norm, orderID, executiondomain.ReasonAdapterSelectionUnavailable)}
	}

	clientOrderID := buildClientOrderID(norm.nowMs, norm.intentID)
	orderReq := TestOrderRequest{
		Symbol:        norm.symbol,
		Side:          sideToBinance(intent.Side),
		OrderType:     strings.ToUpper(string(intent.Constraints.OrderType)),
		TimeInForce:   strings.ToUpper(string(intent.Constraints.TimeInForce)),
		Quantity:      math.Abs(norm.requestedQty),
		LimitPrice:    intent.Constraints.LimitPrice,
		ClientOrderID: clientOrderID,
		TimestampMs:   norm.nowMs,
	}
	mode := strings.ToLower(strings.TrimSpace(e.cfg.EndpointMode))
	switch mode {
	case endpointModeTestOrder:
		venueOrderID, err := e.gateway.SubmitTestOrder(context.Background(), orderReq)
		if err != nil {
			return []executiondomain.ExecutionEventV1{e.rejectEvent(streamKey, norm, orderID, credentialReasonForError(err))}
		}
		if strings.TrimSpace(venueOrderID) == "" {
			venueOrderID = "BN-TEST-UNKNOWN"
		}
		acceptedSeq := e.nextSeq(streamKey)
		return []executiondomain.ExecutionEventV1{{
			EventID:      eventID(norm.intentID, executiondomain.ExecutionStatusAccepted, acceptedSeq),
			Status:       executiondomain.ExecutionStatusAccepted,
			Correlation:  buildCorrelation(norm, orderID, clientOrderID, venueOrderID),
			TsEventMs:    norm.nowMs,
			TsExchangeMs: norm.nowMs,
			ExecutionSeq: acceptedSeq,
			Attempt:      1,
			RequestedQty: norm.requestedQty,
			LeavesQty:    math.Abs(norm.requestedQty),
			LimitPrice:   maxFloat(intent.Constraints.LimitPrice, 0),
			Reason:       reasonAcceptedRealTestOrder,
			Provenance: executiondomain.ExecutionProvenance{
				CorrelationID: norm.correlationID,
				TraceID:       norm.traceID,
				Source:        e.cfg.Source,
			},
		}}
	case endpointModeSafeLifecycle:
		placedSnapshot, err := e.gateway.SubmitOrder(context.Background(), orderReq)
		if err != nil {
			return []executiondomain.ExecutionEventV1{e.rejectEvent(streamKey, norm, orderID, credentialReasonForError(err))}
		}
		if strings.TrimSpace(placedSnapshot.VenueOrderID) == "" {
			placedSnapshot.VenueOrderID = "BN-UNKNOWN"
		}
		if strings.TrimSpace(placedSnapshot.ClientOrderID) == "" {
			placedSnapshot.ClientOrderID = clientOrderID
		}
		correlation := buildCorrelation(norm, orderID, placedSnapshot.ClientOrderID, placedSnapshot.VenueOrderID)
		requestedAbs := math.Abs(norm.requestedQty)
		lastEventTs := nextLifecycleEventTs(0, norm.nowMs, norm.nowMs)
		progress := lifecycleProgress{
			status:           executiondomain.ExecutionStatusAccepted,
			cumulativeSigned: 0,
			leavesQty:        requestedAbs,
			lastEventTsMs:    lastEventTs,
		}

		out := make([]executiondomain.ExecutionEventV1, 0, 8)
		acceptedSeq := e.nextSeq(streamKey)
		out = append(out, executiondomain.ExecutionEventV1{
			EventID:      eventID(norm.intentID, executiondomain.ExecutionStatusAccepted, acceptedSeq),
			Status:       executiondomain.ExecutionStatusAccepted,
			Correlation:  correlation,
			TsEventMs:    lastEventTs,
			TsExchangeMs: placedSnapshot.TsExchangeMs,
			ExecutionSeq: acceptedSeq,
			Attempt:      1,
			RequestedQty: norm.requestedQty,
			LeavesQty:    requestedAbs,
			LimitPrice:   maxFloat(intent.Constraints.LimitPrice, 0),
			Reason:       reasonAcceptedRealLifecycle,
			Provenance: executiondomain.ExecutionProvenance{
				CorrelationID: norm.correlationID,
				TraceID:       norm.traceID,
				Source:        e.cfg.Source,
			},
		})

		out, progress, terminal := e.appendLifecycleFromSnapshot(out, streamKey, norm, correlation, placedSnapshot, progress)
		if terminal {
			return out
		}
		if !e.cfg.ReconcileEnabled {
			return out
		}

		for poll := 0; poll < e.cfg.ReconcileMaxPolls; poll++ {
			if poll > 0 && e.cfg.ReconcilePollEvery > 0 {
				time.Sleep(e.cfg.ReconcilePollEvery)
			}
			snapshot, err := e.gateway.QueryOrder(
				context.Background(),
				norm.symbol,
				correlation.VenueOrderID,
				correlation.ClientOrderID,
				progress.lastEventTsMs,
			)
			if err != nil {
				out = append(out, e.lifecycleFailureEvent(streamKey, norm, correlation, progress, reasonFailedReconciliationPoll))
				return out
			}
			out, progress, terminal = e.appendLifecycleFromSnapshot(out, streamKey, norm, correlation, snapshot, progress)
			if terminal {
				return out
			}
		}
		out = append(out, e.lifecycleFailureEvent(streamKey, norm, correlation, progress, reasonFailedReconciliationTimeout))
		return out
	default:
		return []executiondomain.ExecutionEventV1{e.rejectEvent(streamKey, norm, orderID, reasonRejectedEndpointModeUnsupported)}
	}
}

//nolint:gocyclo // Validation stays branch-per-invariant so rejection reasons remain deterministic.
func (e *SafeIntentExecutor) rejectionReason(intent strategydomain.StrategyIntentV1) string {
	if strings.TrimSpace(intent.IntentID) == "" ||
		strings.TrimSpace(intent.Scope.Venue) == "" ||
		strings.TrimSpace(intent.Scope.Symbol) == "" {
		return reasonRejectedIntentIncomplete
	}
	switch strings.ToLower(strings.TrimSpace(e.cfg.EndpointMode)) {
	case endpointModeTestOrder:
		if e.cfg.ReconcileEnabled {
			return reasonRejectedEndpointModeUnsupported
		}
	case endpointModeSafeLifecycle:
		if !e.cfg.ReconcileEnabled || e.cfg.ReconcileMaxPolls <= 0 {
			return reasonRejectedReconciliationRequired
		}
	default:
		return reasonRejectedEndpointModeUnsupported
	}
	if intent.Sizing.Mode != strategydomain.SizingModeBaseQuantity {
		return reasonRejectedSizingModeUnsupported
	}
	if intent.Constraints.PostOnly {
		return reasonRejectedPostOnlyUnsupported
	}
	if intent.Constraints.ReduceOnly {
		return reasonRejectedReduceOnlyUnsupported
	}
	switch intent.Constraints.OrderType {
	case strategydomain.OrderTypeMarket, strategydomain.OrderTypeLimit:
	default:
		return reasonRejectedOrderTypeUnsupported
	}
	if intent.Constraints.OrderType == strategydomain.OrderTypeLimit {
		if intent.Constraints.LimitPrice <= 0 {
			return reasonRejectedOrderTypeUnsupported
		}
		switch intent.Constraints.TimeInForce {
		case strategydomain.TimeInForceGTC, strategydomain.TimeInForceIOC, strategydomain.TimeInForceFOK:
		default:
			return reasonRejectedTimeInForceUnsupported
		}
	}
	if p := intent.Validate(); p != nil {
		return reasonRejectedIntentValidationFailed
	}
	return ""
}

type lifecycleProgress struct {
	status           executiondomain.ExecutionStatus
	cumulativeSigned float64
	leavesQty        float64
	lastEventTsMs    int64
}

//nolint:gocyclo // Lifecycle translation keeps exchange-status reconciliation explicit in one place.
func (e *SafeIntentExecutor) appendLifecycleFromSnapshot(
	out []executiondomain.ExecutionEventV1,
	streamKey string,
	norm normalizedIntent,
	correlation executiondomain.ExecutionCorrelation,
	snapshot OrderSnapshot,
	progress lifecycleProgress,
) ([]executiondomain.ExecutionEventV1, lifecycleProgress, bool) {
	status, reason, terminal := mapObservedStatus(snapshot.Status)
	if status == executiondomain.ExecutionStatusUnspecified {
		failed := e.lifecycleFailureEvent(streamKey, norm, correlation, progress, reasonFailedUnknownVenueStatus)
		return append(out, failed), lifecycleProgress{
			status:           failed.Status,
			cumulativeSigned: failed.CumulativeFilledQty,
			leavesQty:        failed.LeavesQty,
			lastEventTsMs:    failed.TsEventMs,
		}, true
	}

	requestedAbs := math.Abs(norm.requestedQty)
	if snapshot.RequestedQty > quantityEpsilon {
		requestedAbs = snapshot.RequestedQty
	}
	cumulativeAbs := maxFloat(snapshot.CumulativeFilledQty, 0)
	if cumulativeAbs > requestedAbs && requestedAbs > 0 {
		cumulativeAbs = requestedAbs
	}
	cumulativeSigned := signedMagnitude(norm.requestedQty, cumulativeAbs)
	leavesQty := requestedAbs - cumulativeAbs
	if snapshot.LeavesQty > quantityEpsilon {
		leavesQty = snapshot.LeavesQty
	}
	if leavesQty < 0 {
		leavesQty = 0
	}
	if status == executiondomain.ExecutionStatusFilled {
		leavesQty = 0
	}

	if status == progress.status &&
		almostEqual(cumulativeSigned, progress.cumulativeSigned) &&
		almostEqual(leavesQty, progress.leavesQty) {
		return out, progress, terminal
	}

	lastFillQty := 0.0
	if status == executiondomain.ExecutionStatusPartiallyFilled || status == executiondomain.ExecutionStatusFilled {
		lastFillQty = cumulativeSigned - progress.cumulativeSigned
		if almostEqual(lastFillQty, 0) {
			if status == executiondomain.ExecutionStatusFilled && !almostEqual(cumulativeSigned, 0) {
				lastFillQty = cumulativeSigned
			} else {
				return out, progress, terminal
			}
		}
	}

	eventTs := nextLifecycleEventTs(progress.lastEventTsMs, snapshot.TsExchangeMs, norm.nowMs)
	seq := e.nextSeq(streamKey)
	observed := executiondomain.ExecutionEventV1{
		EventID:             eventID(norm.intentID, status, seq),
		Status:              status,
		Correlation:         correlation,
		TsEventMs:           eventTs,
		TsExchangeMs:        snapshot.TsExchangeMs,
		ExecutionSeq:        seq,
		Attempt:             1,
		RequestedQty:        norm.requestedQty,
		CumulativeFilledQty: cumulativeSigned,
		LastFillQty:         lastFillQty,
		LeavesQty:           leavesQty,
		LimitPrice:          maxFloat(snapshot.LimitPrice, norm.limitPrice),
		AvgFillPrice:        maxFloat(snapshot.AvgFillPrice, 0),
		LastFillPrice:       maxFloat(snapshot.LastFillPrice, 0),
		Reason:              reason,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: norm.correlationID,
			TraceID:       norm.traceID,
			Source:        e.cfg.Source,
		},
	}
	if observed.AvgFillPrice <= 0 {
		observed.AvgFillPrice = observed.LimitPrice
	}
	if observed.LastFillPrice <= 0 {
		observed.LastFillPrice = observed.AvgFillPrice
	}
	return append(out, observed), lifecycleProgress{
		status:           status,
		cumulativeSigned: cumulativeSigned,
		leavesQty:        leavesQty,
		lastEventTsMs:    eventTs,
	}, terminal
}

func (e *SafeIntentExecutor) lifecycleFailureEvent(
	streamKey string,
	norm normalizedIntent,
	correlation executiondomain.ExecutionCorrelation,
	progress lifecycleProgress,
	reason string,
) executiondomain.ExecutionEventV1 {
	ts := nextLifecycleEventTs(progress.lastEventTsMs, 0, norm.nowMs)
	seq := e.nextSeq(streamKey)
	return executiondomain.ExecutionEventV1{
		EventID:             eventID(norm.intentID, executiondomain.ExecutionStatusFailed, seq),
		Status:              executiondomain.ExecutionStatusFailed,
		Correlation:         correlation,
		TsEventMs:           ts,
		TsExchangeMs:        0,
		ExecutionSeq:        seq,
		Attempt:             1,
		RequestedQty:        norm.requestedQty,
		CumulativeFilledQty: progress.cumulativeSigned,
		LeavesQty:           progress.leavesQty,
		LimitPrice:          norm.limitPrice,
		Reason:              reason,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: norm.correlationID,
			TraceID:       norm.traceID,
			Source:        e.cfg.Source,
		},
	}
}

func mapObservedStatus(raw string) (executiondomain.ExecutionStatus, string, bool) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "NEW", "PENDING_NEW", "PENDING_CANCEL":
		return executiondomain.ExecutionStatusPlaced, reasonPlacedObserved, false
	case "PARTIALLY_FILLED":
		return executiondomain.ExecutionStatusPartiallyFilled, reasonPartiallyFilledObserved, false
	case "FILLED":
		return executiondomain.ExecutionStatusFilled, reasonFilledObserved, true
	case "CANCELED":
		return executiondomain.ExecutionStatusCanceled, reasonCanceledObserved, true
	case "EXPIRED":
		return executiondomain.ExecutionStatusExpired, reasonExpiredObserved, true
	case "REJECTED":
		return executiondomain.ExecutionStatusFailed, reasonFailedObserved, true
	default:
		return executiondomain.ExecutionStatusUnspecified, "", true
	}
}

func nextLifecycleEventTs(previous, candidate, fallback int64) int64 {
	ts := candidate
	if ts <= 0 {
		ts = fallback
	}
	if ts <= 0 {
		ts = 1
	}
	if ts <= previous {
		ts = previous + 1
	}
	return ts
}

func signedMagnitude(referenceQty, magnitude float64) float64 {
	magnitude = math.Abs(magnitude)
	if referenceQty < 0 {
		return -magnitude
	}
	return magnitude
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= quantityEpsilon
}

func (e *SafeIntentExecutor) rejectEvent(streamKey string, norm normalizedIntent, orderID, reason string) executiondomain.ExecutionEventV1 {
	rejectSeq := e.nextSeq(streamKey)
	return executiondomain.ExecutionEventV1{
		EventID:      eventID(norm.intentID, executiondomain.ExecutionStatusRejected, rejectSeq),
		Status:       executiondomain.ExecutionStatusRejected,
		Correlation:  buildCorrelation(norm, orderID, buildClientOrderID(norm.nowMs, norm.intentID), "BN-REJECTED"),
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
			Source:        e.cfg.Source,
		},
	}
}

func (e *SafeIntentExecutor) nextSeq(streamKey string) int64 {
	next := e.streamSeq[streamKey] + 1
	e.streamSeq[streamKey] = next
	return next
}

func credentialReasonForError(err error) string {
	if reason := executioncred.ReasonFromError(err); strings.TrimSpace(reason) != "" {
		return reason
	}
	if errors.Is(err, ErrTradeCredentialsUnavailable) {
		return executiondomain.ReasonCredentialsUnavailableMaterialMissing
	}
	if errors.Is(err, ErrTradeOnlyScopeRequired) {
		return executiondomain.ReasonCredentialsInvalidTradeOnlyRequired
	}
	return executiondomain.ReasonVenueRuntimeAdapterCallFailed
}

func sideToBinance(side strategydomain.IntentSide) string {
	switch side {
	case strategydomain.IntentSideSell:
		return "SELL"
	default:
		return "BUY"
	}
}

func buildClientOrderID(nowMs int64, intentID string) string {
	base := "MR-" + strconv.FormatInt(nowMs, 10) + "-" + strings.ToUpper(sharedhash.HashFieldsFast("client-order", intentID))
	if len(base) > 32 {
		return base[:32]
	}
	return base
}

func buildCorrelation(intent normalizedIntent, orderID, clientOrderID, venueOrderID string) executiondomain.ExecutionCorrelation {
	return executiondomain.ExecutionCorrelation{
		IntentID:      intent.intentID,
		OrderID:       orderID,
		VenueOrderID:  venueOrderID,
		ClientOrderID: clientOrderID,
		Venue:         intent.venue,
		Symbol:        intent.symbol,
		AccountID:     intent.accountID,
	}
}

func executionStreamKey(venue, symbol, accountID string) string {
	return strings.ToLower(strings.TrimSpace(venue)) + "|" + strings.ToUpper(strings.TrimSpace(symbol)) + "|" + strings.TrimSpace(accountID)
}

func eventID(intentID string, status executiondomain.ExecutionStatus, seq int64) string {
	return sharedhash.HashFieldsFast("execution-event-v1", intentID, string(status), strconv.FormatInt(seq, 10))
}

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

func normalizeIntent(intent strategydomain.StrategyIntentV1, observedAtMs int64) normalizedIntent {
	venue := strings.ToLower(strings.TrimSpace(intent.Scope.Venue))
	if venue == "" {
		venue = "unknown"
	}
	symbol := canonicalExecutionSymbol(intent.Scope.Symbol)
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
			"real-adapter-intent-fallback",
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
		correlationID = sharedhash.HashFieldsFast("real-adapter-correlation", intentID)
	}
	requestedQty := normalizedRequestedQty(intent.Side, intent.Sizing.Value)
	limitPrice := maxFloat(intent.Constraints.LimitPrice, 0)

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

func maxFloat(v, floor float64) float64 {
	if !finite(v) || v < floor {
		return floor
	}
	return v
}

func canonicalExecutionSymbol(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	out := make([]rune, 0, len(trimmed))
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r-'a'+'A')
		case r >= 'A' && r <= 'Z':
			out = append(out, r)
		case r >= '0' && r <= '9':
			out = append(out, r)
		}
	}
	return string(out)
}
