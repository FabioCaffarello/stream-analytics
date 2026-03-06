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

// SimulationConfig controls the deterministic simulation engine behavior.
// All delays are in milliseconds. All probabilities are in [0,1].
type SimulationConfig struct {
	Source        string
	Boundary      string
	AdapterID     string
	ExecutionMode string

	// Latency model: deterministic delays per lifecycle stage.
	AcceptDelayMs    int64 // Delay before accepted event (default: 1).
	PlaceDelayMs     int64 // Delay between accepted and placed (default: 5).
	FillBaseDelayMs  int64 // Base delay between placed and first fill (default: 10).
	FillStepDelayMs  int64 // Delay between consecutive partial fills (default: 8).
	CancelDelayMs    int64 // Delay for cancel event after last partial (default: 5).

	// Fill model configuration.
	MaxPartialFills int     // Maximum number of partial fills for GTC orders (default: 3).
	FillRatio       float64 // Base fill ratio per step [0,1] (default: 0.4, meaning 40% per fill).
	PriceImpactBps  float64 // Simulated price impact in basis points per fill (default: 2).

	// Cancellation model: deterministic cancel probability for GTC limit orders.
	// Probability is evaluated via a deterministic hash of the intent ID.
	CancelProbability float64 // [0,1] probability of partial cancel for GTC (default: 0.15).

	// Rejection limits (same semantics as BootstrapConfig).
	MaxIntentTTLms int64
	MaxAbsQuantity float64
	MaxNotionalUSD float64
	MaxSlippageBps float64
}

// DefaultSimulationConfig returns production-safe defaults for the simulation engine.
func DefaultSimulationConfig() SimulationConfig {
	return SimulationConfig{
		Source:            "executor.simulation.v1",
		Boundary:          "execution.adapter",
		AdapterID:         "simulation.deterministic",
		ExecutionMode:     "bootstrap_simulated",
		AcceptDelayMs:     1,
		PlaceDelayMs:      5,
		FillBaseDelayMs:   10,
		FillStepDelayMs:   8,
		CancelDelayMs:     5,
		MaxPartialFills:   3,
		FillRatio:         0.4,
		PriceImpactBps:    2,
		CancelProbability: 0.15,
		MaxIntentTTLms:    120_000,
		MaxAbsQuantity:    25,
		MaxNotionalUSD:    25_000,
		MaxSlippageBps:    75,
	}
}

// SimulationEngine is a deterministic execution engine that generates realistic
// order lifecycle events (accepted → placed → partial_fill(s) → filled/canceled/expired)
// without connecting to any exchange. All decisions are derived from intent fields
// and the observedAtMs clock, making every execution fully reproducible.
type SimulationEngine struct {
	cfg       SimulationConfig
	streamSeq map[string]int64
}

// NewSimulationEngine creates a simulation engine with the given config.
// Zero-value fields are replaced with defaults.
func NewSimulationEngine(cfg SimulationConfig) *SimulationEngine {
	if strings.TrimSpace(cfg.Source) == "" {
		cfg.Source = "executor.simulation.v1"
	}
	if strings.TrimSpace(cfg.Boundary) == "" {
		cfg.Boundary = "execution.adapter"
	}
	if strings.TrimSpace(cfg.AdapterID) == "" {
		cfg.AdapterID = "simulation.deterministic"
	}
	if strings.TrimSpace(cfg.ExecutionMode) == "" {
		cfg.ExecutionMode = "bootstrap_simulated"
	}
	if cfg.AcceptDelayMs <= 0 {
		cfg.AcceptDelayMs = 1
	}
	if cfg.PlaceDelayMs <= 0 {
		cfg.PlaceDelayMs = 5
	}
	if cfg.FillBaseDelayMs <= 0 {
		cfg.FillBaseDelayMs = 10
	}
	if cfg.FillStepDelayMs <= 0 {
		cfg.FillStepDelayMs = 8
	}
	if cfg.CancelDelayMs <= 0 {
		cfg.CancelDelayMs = 5
	}
	if cfg.MaxPartialFills <= 0 {
		cfg.MaxPartialFills = 3
	}
	if cfg.FillRatio <= 0 || cfg.FillRatio > 1 {
		cfg.FillRatio = 0.4
	}
	if cfg.PriceImpactBps < 0 {
		cfg.PriceImpactBps = 2
	}
	if cfg.CancelProbability < 0 || cfg.CancelProbability > 1 {
		cfg.CancelProbability = 0.15
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
	return &SimulationEngine{
		cfg:       cfg,
		streamSeq: make(map[string]int64),
	}
}

func (s *SimulationEngine) Execute(intent strategydomain.StrategyIntentV1) []executiondomain.ExecutionEventV1 {
	return s.ExecuteAt(intent, 0)
}

func (s *SimulationEngine) BoundaryInfo() executionports.BoundaryInfo {
	return executionports.BoundaryInfo{
		Boundary: strings.TrimSpace(s.cfg.Boundary),
		Adapter:  strings.TrimSpace(s.cfg.AdapterID),
		Mode:     strings.TrimSpace(s.cfg.ExecutionMode),
	}
}

// ExecuteAt generates a deterministic sequence of execution events that model
// a realistic order lifecycle. The sequence depends on:
//   - Order type (market vs limit)
//   - Time-in-force (IOC: immediate fill-or-cancel, FOK: all-or-nothing, GTC: partial fills)
//   - Deterministic hash of intent ID (for cancel/fill decisions)
//   - observedAtMs as the reference clock
func (s *SimulationEngine) ExecuteAt(intent strategydomain.StrategyIntentV1, observedAtMs int64) []executiondomain.ExecutionEventV1 {
	norm := normalizeIntent(intent, observedAtMs)
	streamKey := executionStreamKey(norm.venue, norm.symbol, norm.accountID)
	orderID := sharedhash.HashFieldsFast("simulation-order", norm.intentID)

	// Phase 1: rejection check (same policy as bootstrap).
	if reason := s.rejectionReason(intent, norm); reason != "" {
		rejectSeq := s.nextSeq(streamKey)
		return []executiondomain.ExecutionEventV1{s.buildEvent(
			norm, orderID, executiondomain.ExecutionStatusRejected, rejectSeq,
			norm.nowMs, 0,
			norm.requestedQty, 0, 0, math.Abs(norm.requestedQty),
			norm.limitPrice, 0, 0,
			reason,
		)}
	}

	// Phase 2: accepted event.
	acceptTs := norm.nowMs + s.cfg.AcceptDelayMs
	acceptSeq := s.nextSeq(streamKey)
	accepted := s.buildEvent(
		norm, orderID, executiondomain.ExecutionStatusAccepted, acceptSeq,
		acceptTs, 0,
		norm.requestedQty, 0, 0, math.Abs(norm.requestedQty),
		norm.limitPrice, 0, 0,
		simReasonAccepted,
	)
	out := []executiondomain.ExecutionEventV1{accepted}

	// Phase 3: placed event (order acknowledged by simulated venue).
	placeTs := acceptTs + s.cfg.PlaceDelayMs
	placeSeq := s.nextSeq(streamKey)
	placed := s.buildEvent(
		norm, orderID, executiondomain.ExecutionStatusPlaced, placeSeq,
		placeTs, placeTs,
		norm.requestedQty, 0, 0, math.Abs(norm.requestedQty),
		norm.limitPrice, 0, 0,
		simReasonPlaced,
	)
	out = append(out, placed)

	// Phase 4: fill sequence depends on order type + time-in-force.
	basePrice := deterministicFillPrice(intent, norm.requestedQty)
	absRequestedQty := math.Abs(norm.requestedQty)

	switch intent.Constraints.TimeInForce {
	case strategydomain.TimeInForceFOK:
		out = append(out, s.simulateFOK(norm, orderID, streamKey, placeTs, basePrice, absRequestedQty)...)
	case strategydomain.TimeInForceIOC:
		out = append(out, s.simulateIOC(norm, orderID, streamKey, placeTs, basePrice, absRequestedQty)...)
	default: // GTC
		out = append(out, s.simulateGTC(norm, orderID, streamKey, placeTs, basePrice, absRequestedQty, intent)...)
	}

	return out
}

// simulateFOK: Fill-or-Kill — either fills entirely or cancels entirely.
// Decision is deterministic based on intent hash.
func (s *SimulationEngine) simulateFOK(
	norm normalizedIntent, orderID string, streamKey string,
	afterTs int64, basePrice, absQty float64,
) []executiondomain.ExecutionEventV1 {
	fillTs := afterTs + s.cfg.FillBaseDelayMs

	// Deterministic fill decision: FOK succeeds unless cancel hash triggers.
	if s.shouldCancel(norm.intentID, "fok") {
		cancelSeq := s.nextSeq(streamKey)
		return []executiondomain.ExecutionEventV1{s.buildEvent(
			norm, orderID, executiondomain.ExecutionStatusCanceled, cancelSeq,
			fillTs, fillTs,
			norm.requestedQty, 0, 0, absQty,
			norm.limitPrice, 0, 0,
			simReasonFOKNoFill,
		)}
	}

	fillSeq := s.nextSeq(streamKey)
	fillPrice := s.impactPrice(basePrice, norm.requestedQty, 1)
	return []executiondomain.ExecutionEventV1{s.buildEvent(
		norm, orderID, executiondomain.ExecutionStatusFilled, fillSeq,
		fillTs, fillTs,
		norm.requestedQty, norm.requestedQty, norm.requestedQty, 0,
		norm.limitPrice, fillPrice, fillPrice,
		simReasonFilled,
	)}
}

// simulateIOC: Immediate-or-Cancel — fills what it can immediately, cancels remainder.
// Uses deterministic fill ratio from intent hash.
func (s *SimulationEngine) simulateIOC(
	norm normalizedIntent, orderID string, streamKey string,
	afterTs int64, basePrice, absQty float64,
) []executiondomain.ExecutionEventV1 {
	fillTs := afterTs + s.cfg.FillBaseDelayMs
	fillRatio := s.deterministicFillRatio(norm.intentID, "ioc")

	if fillRatio >= 1.0-1e-9 {
		// Full fill.
		fillSeq := s.nextSeq(streamKey)
		fillPrice := s.impactPrice(basePrice, norm.requestedQty, 1)
		return []executiondomain.ExecutionEventV1{s.buildEvent(
			norm, orderID, executiondomain.ExecutionStatusFilled, fillSeq,
			fillTs, fillTs,
			norm.requestedQty, norm.requestedQty, norm.requestedQty, 0,
			norm.limitPrice, fillPrice, fillPrice,
			simReasonFilled,
		)}
	}

	var events []executiondomain.ExecutionEventV1

	// Partial fill.
	fillQtyAbs := roundQty(absQty * fillRatio)
	if fillQtyAbs < 1e-9 {
		// Nothing filled — cancel entirely.
		cancelSeq := s.nextSeq(streamKey)
		return []executiondomain.ExecutionEventV1{s.buildEvent(
			norm, orderID, executiondomain.ExecutionStatusCanceled, cancelSeq,
			fillTs, fillTs,
			norm.requestedQty, 0, 0, absQty,
			norm.limitPrice, 0, 0,
			simReasonIOCNoFill,
		)}
	}

	signedFillQty := math.Copysign(fillQtyAbs, norm.requestedQty)
	leavesQty := absQty - fillQtyAbs
	fillPrice := s.impactPrice(basePrice, norm.requestedQty, 1)

	partialSeq := s.nextSeq(streamKey)
	events = append(events, s.buildEvent(
		norm, orderID, executiondomain.ExecutionStatusPartiallyFilled, partialSeq,
		fillTs, fillTs,
		norm.requestedQty, signedFillQty, signedFillQty, leavesQty,
		norm.limitPrice, fillPrice, fillPrice,
		simReasonPartialFill,
	))

	// Cancel remainder.
	cancelTs := fillTs + s.cfg.CancelDelayMs
	cancelSeq := s.nextSeq(streamKey)
	events = append(events, s.buildEvent(
		norm, orderID, executiondomain.ExecutionStatusCanceled, cancelSeq,
		cancelTs, cancelTs,
		norm.requestedQty, signedFillQty, 0, leavesQty,
		norm.limitPrice, fillPrice, 0,
		simReasonIOCCancelRemainder,
	))

	return events
}

// simulateGTC: Good-til-Canceled — fills over multiple steps with optional
// partial cancellation. Market orders always fill fully; limit orders may
// partially fill and then get canceled or expire.
func (s *SimulationEngine) simulateGTC(
	norm normalizedIntent, orderID string, streamKey string,
	afterTs int64, basePrice, absQty float64,
	intent strategydomain.StrategyIntentV1,
) []executiondomain.ExecutionEventV1 {
	// Market orders: always fill fully in one step.
	if intent.Constraints.OrderType == strategydomain.OrderTypeMarket {
		fillTs := afterTs + s.cfg.FillBaseDelayMs
		fillSeq := s.nextSeq(streamKey)
		fillPrice := s.impactPrice(basePrice, norm.requestedQty, 1)
		return []executiondomain.ExecutionEventV1{s.buildEvent(
			norm, orderID, executiondomain.ExecutionStatusFilled, fillSeq,
			fillTs, fillTs,
			norm.requestedQty, norm.requestedQty, norm.requestedQty, 0,
			norm.limitPrice, fillPrice, fillPrice,
			simReasonFilled,
		)}
	}

	// Limit orders: partial fills over time.
	numFills := s.deterministicFillCount(norm.intentID)
	willCancel := s.shouldCancel(norm.intentID, "gtc")
	willExpire := s.shouldExpire(norm, intent)

	var events []executiondomain.ExecutionEventV1
	cumulativeFilledAbs := 0.0
	var weightedPriceSum float64
	currentTs := afterTs

	for i := 0; i < numFills; i++ {
		isLast := i == numFills-1
		remaining := absQty - cumulativeFilledAbs
		if remaining < 1e-9 {
			break
		}

		// Determine fill quantity for this step.
		var stepFillAbs float64
		if isLast && !willCancel && !willExpire {
			// Final fill: fill the rest.
			stepFillAbs = remaining
		} else {
			stepFillAbs = roundQty(remaining * s.cfg.FillRatio)
			if stepFillAbs < 1e-9 {
				stepFillAbs = remaining
			}
		}
		if stepFillAbs > remaining {
			stepFillAbs = remaining
		}

		cumulativeFilledAbs += stepFillAbs
		leavesQty := absQty - cumulativeFilledAbs
		if leavesQty < 1e-9 {
			leavesQty = 0
		}

		signedStepFill := math.Copysign(stepFillAbs, norm.requestedQty)
		signedCumulative := math.Copysign(cumulativeFilledAbs, norm.requestedQty)

		stepPrice := s.impactPrice(basePrice, norm.requestedQty, i+1)
		weightedPriceSum += stepFillAbs * stepPrice
		avgPrice := weightedPriceSum / cumulativeFilledAbs

		if i == 0 {
			currentTs += s.cfg.FillBaseDelayMs
		} else {
			currentTs += s.cfg.FillStepDelayMs
		}

		// Check TTL expiry before emitting fill.
		if willExpire && currentTs >= intent.ExpiresAtMs {
			expirySeq := s.nextSeq(streamKey)
			events = append(events, s.buildEvent(
				norm, orderID, executiondomain.ExecutionStatusExpired, expirySeq,
				intent.ExpiresAtMs, intent.ExpiresAtMs,
				norm.requestedQty, math.Copysign(cumulativeFilledAbs-stepFillAbs, norm.requestedQty), 0,
				absQty-(cumulativeFilledAbs-stepFillAbs),
				norm.limitPrice, avgPrice, 0,
				simReasonExpired,
			))
			return events
		}

		terminal := leavesQty < 1e-9
		status := executiondomain.ExecutionStatusPartiallyFilled
		reason := simReasonPartialFill
		if terminal {
			status = executiondomain.ExecutionStatusFilled
			reason = simReasonFilled
		}

		fillSeq := s.nextSeq(streamKey)
		events = append(events, s.buildEvent(
			norm, orderID, status, fillSeq,
			currentTs, currentTs,
			norm.requestedQty, signedCumulative, signedStepFill, leavesQty,
			norm.limitPrice, avgPrice, stepPrice,
			reason,
		))

		if terminal {
			return events
		}
	}

	// Post-fill: cancel remainder if flagged.
	if willCancel && cumulativeFilledAbs < absQty {
		cancelTs := currentTs + s.cfg.CancelDelayMs
		cancelSeq := s.nextSeq(streamKey)
		leavesQty := absQty - cumulativeFilledAbs
		avgPrice := 0.0
		if cumulativeFilledAbs > 1e-9 {
			avgPrice = weightedPriceSum / cumulativeFilledAbs
		}
		events = append(events, s.buildEvent(
			norm, orderID, executiondomain.ExecutionStatusCanceled, cancelSeq,
			cancelTs, cancelTs,
			norm.requestedQty, math.Copysign(cumulativeFilledAbs, norm.requestedQty), 0, leavesQty,
			norm.limitPrice, avgPrice, 0,
			simReasonCanceledPartial,
		))
		return events
	}

	// If we get here, all fills consumed the full quantity (shouldn't happen
	// given the loop logic, but defensive).
	return events
}

// ---------- deterministic decision functions ----------

// shouldCancel returns a deterministic boolean based on the intent ID hash
// and the cancel probability configured for this engine.
func (s *SimulationEngine) shouldCancel(intentID, context string) bool {
	h := deterministicHash(intentID, "cancel", context)
	return h < s.cfg.CancelProbability
}

// shouldExpire checks if the intent's TTL window is tight enough that
// simulated fills would exceed the expiry. This is deterministic.
func (s *SimulationEngine) shouldExpire(norm normalizedIntent, intent strategydomain.StrategyIntentV1) bool {
	totalFillTime := s.cfg.AcceptDelayMs + s.cfg.PlaceDelayMs + s.cfg.FillBaseDelayMs +
		int64(s.cfg.MaxPartialFills-1)*s.cfg.FillStepDelayMs
	return intent.ExpiresAtMs-norm.nowMs < totalFillTime
}

// deterministicFillRatio returns a fill ratio in [FillRatio, 1.0] based on
// a deterministic hash of the intent ID.
func (s *SimulationEngine) deterministicFillRatio(intentID, context string) float64 {
	h := deterministicHash(intentID, "fill-ratio", context)
	// Map hash to [FillRatio, 1.0] range.
	return s.cfg.FillRatio + h*(1.0-s.cfg.FillRatio)
}

// deterministicFillCount returns the number of fills [1, MaxPartialFills]
// based on a deterministic hash of the intent ID.
func (s *SimulationEngine) deterministicFillCount(intentID string) int {
	h := deterministicHash(intentID, "fill-count", "gtc")
	count := 1 + int(h*float64(s.cfg.MaxPartialFills))
	if count > s.cfg.MaxPartialFills {
		count = s.cfg.MaxPartialFills
	}
	if count < 1 {
		count = 1
	}
	return count
}

// impactPrice applies deterministic price impact to the base price.
// Each fill step adds incremental slippage in basis points.
func (s *SimulationEngine) impactPrice(basePrice, requestedQty float64, step int) float64 {
	if s.cfg.PriceImpactBps <= 0 || basePrice <= 0 {
		return basePrice
	}
	impactFraction := s.cfg.PriceImpactBps * float64(step) / 10000.0
	if requestedQty > 0 {
		// Buy: price goes up.
		return roundPrice(basePrice * (1 + impactFraction))
	}
	// Sell: price goes down.
	return roundPrice(basePrice * (1 - impactFraction))
}

// deterministicHash returns a float64 in [0, 1) derived from FNV-1a hash
// of the input fields. This is the engine's only source of "randomness" —
// it is fully reproducible given the same inputs.
func deterministicHash(fields ...string) float64 {
	h := sharedhash.HashFieldsFast(fields...)
	// Use first 8 hex chars of the hash string as a uint32.
	hexPart := h
	if len(hexPart) > 8 {
		hexPart = hexPart[:8]
	}
	val, err := strconv.ParseUint(hexPart, 16, 64)
	if err != nil {
		// Fallback: hash the entire string byte by byte (FNV-1a manual).
		var acc uint64
		for i := 0; i < len(h); i++ {
			acc = acc*31 + uint64(h[i])
		}
		return float64(acc%10000) / 10000.0
	}
	return float64(val%10000) / 10000.0
}

// ---------- event builder ----------

func (s *SimulationEngine) buildEvent(
	norm normalizedIntent, orderID string,
	status executiondomain.ExecutionStatus, seq int64,
	tsEvent, tsExchange int64,
	requestedQty, cumulativeFilled, lastFill, leavesQty float64,
	limitPrice, avgFillPrice, lastFillPrice float64,
	reason string,
) executiondomain.ExecutionEventV1 {
	venueOrderID := "SIM-" + strings.ToUpper(orderID)
	if len(venueOrderID) > 20 {
		venueOrderID = venueOrderID[:20]
	}
	return executiondomain.ExecutionEventV1{
		EventID: eventID(norm.intentID, status, seq),
		Status:  status,
		Correlation: executiondomain.ExecutionCorrelation{
			IntentID:      norm.intentID,
			OrderID:       orderID,
			VenueOrderID:  venueOrderID,
			ClientOrderID: "CID-" + strconv.FormatInt(norm.nowMs, 10),
			Venue:         norm.venue,
			Symbol:        norm.symbol,
			AccountID:     norm.accountID,
		},
		TsEventMs:           tsEvent,
		TsExchangeMs:        tsExchange,
		ExecutionSeq:        seq,
		Attempt:             1,
		RequestedQty:        requestedQty,
		CumulativeFilledQty: cumulativeFilled,
		LastFillQty:         lastFill,
		LeavesQty:           leavesQty,
		LimitPrice:          limitPrice,
		AvgFillPrice:        avgFillPrice,
		LastFillPrice:       lastFillPrice,
		Reason:              reason,
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: norm.correlationID,
			TraceID:       norm.traceID,
			Source:        s.cfg.Source,
		},
	}
}

func (s *SimulationEngine) nextSeq(streamKey string) int64 {
	next := s.streamSeq[streamKey] + 1
	s.streamSeq[streamKey] = next
	return next
}

//nolint:gocyclo // explicit deterministic policy order is intentional.
func (s *SimulationEngine) rejectionReason(intent strategydomain.StrategyIntentV1, norm normalizedIntent) string {
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
	if intent.ExpiresAtMs-intent.CreatedAtMs > s.cfg.MaxIntentTTLms {
		return executionReasonTTLTooLarge
	}
	if math.Abs(intent.Sizing.Value) > s.cfg.MaxAbsQuantity {
		return executionReasonSizeTooLarge
	}
	if intent.Sizing.MaxNotionalUSD > s.cfg.MaxNotionalUSD {
		return executionReasonNotionalTooLarge
	}
	if intent.Constraints.MaxSlippageBps > s.cfg.MaxSlippageBps {
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

// ---------- simulation-specific reason constants ----------

const (
	simReasonAccepted          = "accepted_simulation_policy"
	simReasonPlaced            = "placed_simulated_venue"
	simReasonFilled            = "filled_simulation_complete"
	simReasonPartialFill       = "partially_filled_simulation"
	simReasonFOKNoFill         = "canceled_fok_no_liquidity"
	simReasonIOCNoFill         = "canceled_ioc_no_liquidity"
	simReasonIOCCancelRemainder = "canceled_ioc_remainder"
	simReasonCanceledPartial   = "canceled_simulation_partial"
	simReasonExpired           = "expired_simulation_ttl"
)

// ---------- utility ----------

func roundQty(v float64) float64 {
	return math.Round(v*1e8) / 1e8
}

func roundPrice(v float64) float64 {
	return math.Round(v*1e2) / 1e2
}
