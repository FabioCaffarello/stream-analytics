package app

import (
	"math"
	"strings"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

type ProjectorConfig struct {
	InitialQuoteBalance float64
	ProjectorVersion    string
}

func DefaultProjectorConfig() ProjectorConfig {
	return ProjectorConfig{
		InitialQuoteBalance: 10_000,
		ProjectorVersion:    "portfolio-bootstrap-v1",
	}
}

type positionState struct {
	qty             float64
	avgPrice        float64
	cashUSD         float64
	realized        float64
	lastPrice       float64
	orders          map[string]pendingOrderState
	tradeCount      int32
	volumeTradedUSD float64
	lastFillMs      int64
	winCount        int32
	lossCount       int32
	largestWinUSD   float64
	largestLossUSD  float64
	turnoverUSD     float64
}

type pendingOrderState struct {
	signedQty           float64
	leavesQty           float64
	referencePrice      float64
	cumulativeFilledQty float64
}

type BootstrapProjector struct {
	cfg    ProjectorConfig
	states map[string]positionState
}

func NewBootstrapProjector(cfg ProjectorConfig) *BootstrapProjector {
	if cfg.InitialQuoteBalance == 0 {
		cfg.InitialQuoteBalance = 10_000
	}
	if strings.TrimSpace(cfg.ProjectorVersion) == "" {
		cfg.ProjectorVersion = "portfolio-bootstrap-v1"
	}
	return &BootstrapProjector{
		cfg:    cfg,
		states: make(map[string]positionState),
	}
}

func (p *BootstrapProjector) Apply(event executiondomain.ExecutionEventV1) (portfoliodomain.PortfolioStateV1, bool) {
	if prob := event.Validate(); prob != nil {
		return portfoliodomain.PortfolioStateV1{}, false
	}

	key := portfolioKey(event.Correlation.AccountID, event.Correlation.Venue, event.Correlation.Symbol)
	state, ok := p.states[key]
	if !ok {
		state = positionState{
			cashUSD: p.cfg.InitialQuoteBalance,
			orders:  make(map[string]pendingOrderState),
		}
	}
	if state.orders == nil {
		state.orders = make(map[string]pendingOrderState)
	}

	switch event.Status {
	case executiondomain.ExecutionStatusAccepted, executiondomain.ExecutionStatusPlaced:
		state = applyPendingOrder(state, event)
	case executiondomain.ExecutionStatusRejected,
		executiondomain.ExecutionStatusCanceled,
		executiondomain.ExecutionStatusExpired,
		executiondomain.ExecutionStatusFailed:
		state = clearPendingOrder(state, event.Correlation.OrderID)
	case executiondomain.ExecutionStatusPartiallyFilled, executiondomain.ExecutionStatusFilled:
		state = applyFillOrder(state, event)
	}

	if state.lastPrice <= 0 {
		fillPrice := effectiveFillPrice(event)
		if state.lastPrice <= 0 {
			state.lastPrice = fillPrice
		}
		if state.lastPrice <= 0 {
			state.lastPrice = 1
		}
	}

	p.states[key] = state

	baseAsset, quoteAsset := splitInstrument(event.Correlation.Symbol)
	lockedBase, lockedQuote := lockedBalances(state)
	notional := math.Abs(state.qty) * state.lastPrice
	unrealized := unrealizedPnL(state.qty, state.avgPrice, state.lastPrice)
	equity := state.cashUSD + (state.qty * state.lastPrice)
	marginUsed := (notional * 0.1) + (lockedQuote * 0.05) + (lockedBase * state.lastPrice * 0.05)
	if marginUsed < 0 {
		marginUsed = 0
	}
	marginAvailable := equity - marginUsed
	if marginAvailable < 0 {
		marginAvailable = 0
	}
	leverage := 0.0
	if math.Abs(equity) > 1e-9 {
		leverage = (notional + lockedQuote + (lockedBase * state.lastPrice)) / math.Abs(equity)
	}
	baseAvailable := state.qty - lockedBase
	quoteAvailable := state.cashUSD - lockedQuote

	side := ""
	if state.qty > 1e-9 {
		side = "long"
	} else if state.qty < -1e-9 {
		side = "short"
	}

	portfolio := portfoliodomain.PortfolioStateV1{
		StateID:       sharedhash.HashFieldsFast("portfolio-state-v1", key, event.EventID),
		Scope:         portfoliodomain.PortfolioScopeVenueAccount,
		AccountID:     event.Correlation.AccountID,
		Venue:         event.Correlation.Venue,
		ProjectedAtMs: event.TsEventMs,
		Balances: []portfoliodomain.BalanceV1{
			{Asset: baseAsset, Total: state.qty, Available: baseAvailable, Locked: lockedBase},
			{Asset: quoteAsset, Total: state.cashUSD, Available: quoteAvailable, Locked: lockedQuote},
		},
		Positions: []portfoliodomain.PositionV1{
			{
				Venue:           event.Correlation.Venue,
				Symbol:          event.Correlation.Symbol,
				Quantity:        state.qty,
				AvgEntryPrice:   state.avgPrice,
				NotionalUSD:     notional,
				RealizedPnL:     state.realized,
				UnrealizedPnL:   unrealized,
				TradeCount:      state.tradeCount,
				VolumeTradedUSD: state.volumeTradedUSD,
				LastFillMs:      state.lastFillMs,
				Side:            side,
			},
		},
		Exposures: []portfoliodomain.ExposureV1{
			{
				Symbol:           event.Correlation.Symbol,
				NetQty:           state.qty,
				GrossNotionalUSD: notional + lockedQuote + (lockedBase * state.lastPrice),
				Leverage:         leverage,
			},
		},
		EquityUSD:        equity,
		RealizedPnlUSD:   state.realized,
		UnrealizedPnlUSD: unrealized,
		Risk: portfoliodomain.RiskSnapshotV1{
			MarginUsedUSD:        marginUsed,
			MarginAvailableUSD:   marginAvailable,
			MaintenanceMarginUSD: marginUsed * 0.5,
			Var95USD:             notional * 0.02,
		},
		FillSummary: portfoliodomain.FillSummaryV1{
			TotalTradeCount:      state.tradeCount,
			TotalVolumeTradedUSD: state.volumeTradedUSD,
			WinCount:             state.winCount,
			LossCount:            state.lossCount,
			LargestWinUSD:        state.largestWinUSD,
			LargestLossUSD:       state.largestLossUSD,
			TurnoverUSD:          state.turnoverUSD,
		},
		Provenance: portfoliodomain.ProjectionProvenanceV1{
			SourceExecutionEventID: event.EventID,
			SourceExecutionSeq:     event.ExecutionSeq,
			CorrelationID:          event.Provenance.CorrelationID,
			TraceID:                event.Provenance.TraceID,
			ProjectorVersion:       p.cfg.ProjectorVersion,
		},
	}

	if prob := portfolio.Validate(); prob != nil {
		return portfoliodomain.PortfolioStateV1{}, false
	}
	return portfolio, true
}

func applyPendingOrder(state positionState, event executiondomain.ExecutionEventV1) positionState {
	orderID := strings.TrimSpace(event.Correlation.OrderID)
	if orderID == "" {
		return state
	}
	leaves := math.Abs(event.LeavesQty)
	if leaves <= 1e-9 {
		leaves = math.Abs(event.RequestedQty)
	}
	if leaves <= 1e-9 {
		return state
	}
	referencePrice := reservationPrice(event)
	if referencePrice <= 0 {
		referencePrice = state.lastPrice
	}
	if referencePrice <= 0 {
		referencePrice = 1
	}
	existing := state.orders[orderID]
	cumulativeFilled := math.Abs(event.CumulativeFilledQty)
	if cumulativeFilled <= 1e-9 {
		cumulativeFilled = existing.cumulativeFilledQty
	}
	state.orders[orderID] = pendingOrderState{
		signedQty:           signedOrFallback(event.RequestedQty, event.LastFillQty),
		leavesQty:           leaves,
		referencePrice:      referencePrice,
		cumulativeFilledQty: cumulativeFilled,
	}
	return state
}

func clearPendingOrder(state positionState, orderID string) positionState {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" || state.orders == nil {
		return state
	}
	delete(state.orders, orderID)
	return state
}

func applyFillOrder(state positionState, event executiondomain.ExecutionEventV1) positionState {
	orderID := strings.TrimSpace(event.Correlation.OrderID)
	pending := state.orders[orderID]
	fillQty, cumulativeFilled := inferFillQty(event, pending)
	if math.Abs(fillQty) > 1e-9 {
		fillPrice := effectiveFillPrice(event)
		state = applyFill(state, fillQty, fillPrice, event.TsEventMs)
		state.lastPrice = fillPrice
	}

	if orderID == "" {
		return state
	}
	leaves := math.Abs(event.LeavesQty)
	if event.Status == executiondomain.ExecutionStatusFilled || leaves <= 1e-9 {
		delete(state.orders, orderID)
		return state
	}
	if pending.referencePrice <= 0 {
		pending.referencePrice = reservationPrice(event)
	}
	if pending.referencePrice <= 0 {
		pending.referencePrice = state.lastPrice
	}
	if pending.referencePrice <= 0 {
		pending.referencePrice = 1
	}
	pending.signedQty = signedOrFallback(event.RequestedQty, pending.signedQty)
	pending.leavesQty = leaves
	pending.cumulativeFilledQty = cumulativeFilled
	state.orders[orderID] = pending
	return state
}

func applyFill(state positionState, qtyDelta, fillPrice float64, tsEventMs int64) positionState {
	if fillPrice <= 0 {
		fillPrice = 1
	}
	if math.Abs(qtyDelta) <= 1e-9 {
		return state
	}
	prevQty := state.qty
	nextQty := prevQty + qtyDelta

	fillNotional := math.Abs(qtyDelta) * fillPrice
	state.tradeCount++
	state.volumeTradedUSD += fillNotional
	state.turnoverUSD += fillNotional
	if tsEventMs > state.lastFillMs {
		state.lastFillMs = tsEventMs
	}

	if math.Abs(prevQty) <= 1e-9 || sameSign(prevQty, qtyDelta) {
		notional := (math.Abs(prevQty) * state.avgPrice) + (math.Abs(qtyDelta) * fillPrice)
		if math.Abs(nextQty) > 1e-9 {
			state.avgPrice = notional / math.Abs(nextQty)
		} else {
			state.avgPrice = 0
		}
	} else {
		closed := math.Min(math.Abs(prevQty), math.Abs(qtyDelta))
		pnl := closed * (fillPrice - state.avgPrice) * sign(prevQty)
		state.realized += pnl
		if pnl > 0 {
			state.winCount++
			if pnl > state.largestWinUSD {
				state.largestWinUSD = pnl
			}
		} else if pnl < 0 {
			state.lossCount++
			if pnl < state.largestLossUSD {
				state.largestLossUSD = pnl
			}
		}
		if math.Abs(nextQty) <= 1e-9 {
			state.avgPrice = 0
		} else if !sameSign(prevQty, nextQty) {
			state.avgPrice = fillPrice
		}
	}

	if math.Abs(nextQty) <= 1e-9 {
		nextQty = 0
	}
	state.qty = nextQty
	state.cashUSD -= qtyDelta * fillPrice
	return state
}

func effectiveFillPrice(event executiondomain.ExecutionEventV1) float64 {
	if event.LastFillPrice > 0 {
		return event.LastFillPrice
	}
	if event.AvgFillPrice > 0 {
		return event.AvgFillPrice
	}
	if event.LimitPrice > 0 {
		return event.LimitPrice
	}
	return 1
}

func reservationPrice(event executiondomain.ExecutionEventV1) float64 {
	if event.LimitPrice > 0 {
		return event.LimitPrice
	}
	if event.AvgFillPrice > 0 {
		return event.AvgFillPrice
	}
	if event.LastFillPrice > 0 {
		return event.LastFillPrice
	}
	return 1
}

func inferFillQty(event executiondomain.ExecutionEventV1, pending pendingOrderState) (float64, float64) {
	signRef := signedOrFallback(event.RequestedQty, pending.signedQty)
	currentCumulative := math.Abs(event.CumulativeFilledQty)
	if currentCumulative <= 1e-9 {
		currentCumulative = pending.cumulativeFilledQty
	}
	fillAbsDelta := 0.0
	if math.Abs(event.LastFillQty) > 1e-9 {
		fillAbsDelta = math.Abs(event.LastFillQty)
		if currentCumulative < pending.cumulativeFilledQty+fillAbsDelta {
			currentCumulative = pending.cumulativeFilledQty + fillAbsDelta
		}
	} else {
		if event.Status == executiondomain.ExecutionStatusFilled && currentCumulative <= 1e-9 {
			switch {
			case pending.leavesQty > 1e-9:
				currentCumulative = pending.cumulativeFilledQty + pending.leavesQty
			default:
				currentCumulative = math.Abs(event.RequestedQty)
			}
		}
		fillAbsDelta = currentCumulative - pending.cumulativeFilledQty
	}
	if fillAbsDelta <= 1e-9 {
		return 0, currentCumulative
	}
	return math.Copysign(fillAbsDelta, signRef), currentCumulative
}

func signedOrFallback(primary, fallback float64) float64 {
	if math.Abs(primary) > 1e-9 {
		return primary
	}
	return fallback
}

func lockedBalances(state positionState) (lockedBase float64, lockedQuote float64) {
	for _, order := range state.orders {
		if order.leavesQty <= 0 {
			continue
		}
		if order.signedQty < 0 {
			lockedBase += order.leavesQty
			continue
		}
		price := order.referencePrice
		if price <= 0 {
			price = 1
		}
		lockedQuote += order.leavesQty * price
	}
	return lockedBase, lockedQuote
}

func unrealizedPnL(qty, avgPrice, markPrice float64) float64 {
	if math.Abs(qty) <= 1e-9 || avgPrice <= 0 || markPrice <= 0 {
		return 0
	}
	return qty * (markPrice - avgPrice)
}

func portfolioKey(accountID, venue, symbol string) string {
	return strings.TrimSpace(accountID) + "|" + strings.ToLower(strings.TrimSpace(venue)) + "|" + strings.ToUpper(strings.TrimSpace(symbol))
}

func sameSign(a, b float64) bool {
	return (a >= 0 && b >= 0) || (a <= 0 && b <= 0)
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

func splitInstrument(instrument string) (baseAsset, quoteAsset string) {
	clean := strings.ToUpper(strings.TrimSpace(instrument))
	if clean == "" {
		return "BASE", "USD"
	}
	for _, sep := range []string{"-", "/", "_"} {
		parts := strings.Split(clean, sep)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	for _, quote := range []string{"USDT", "USDC", "BUSD", "USD"} {
		if strings.HasSuffix(clean, quote) && len(clean) > len(quote) {
			return clean[:len(clean)-len(quote)], quote
		}
	}
	return clean, "USD"
}
