package contracts

import (
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
	executionv1 "github.com/market-raccoon/internal/shared/proto/gen/execution/v1"
	portfoliov1 "github.com/market-raccoon/internal/shared/proto/gen/portfolio/v1"
	strategyv1 "github.com/market-raccoon/internal/shared/proto/gen/strategy/v1"
)

func DomainToProtoStrategyIntentV1(in strategydomain.StrategyIntentV1) *strategyv1.StrategyIntentV1 {
	parentSignalIDs := append([]string(nil), in.Provenance.ParentSignalIDs...)
	return &strategyv1.StrategyIntentV1{
		IntentId: in.IntentID,
		Strategy: &strategyv1.StrategyRef{
			StrategyId:         in.Strategy.StrategyID,
			StrategyVersion:    in.Strategy.StrategyVersion,
			StrategyInstanceId: in.Strategy.StrategyInstanceID,
		},
		Scope: &strategyv1.IntentScope{
			Venue:     in.Scope.Venue,
			Symbol:    in.Scope.Symbol,
			AccountId: in.Scope.AccountID,
		},
		Side: sideDomainToProto(in.Side),
		Sizing: &strategyv1.SizingIntent{
			Mode:           sizingModeDomainToProto(in.Sizing.Mode),
			Value:          in.Sizing.Value,
			MaxNotionalUsd: in.Sizing.MaxNotionalUSD,
		},
		Constraints: &strategyv1.ExecutionConstraints{
			OrderType:      orderTypeDomainToProto(in.Constraints.OrderType),
			TimeInForce:    tifDomainToProto(in.Constraints.TimeInForce),
			LimitPrice:     in.Constraints.LimitPrice,
			MaxSlippageBps: in.Constraints.MaxSlippageBps,
			PostOnly:       in.Constraints.PostOnly,
			ReduceOnly:     in.Constraints.ReduceOnly,
		},
		CreatedAtMs: in.CreatedAtMs,
		ExpiresAtMs: in.ExpiresAtMs,
		Provenance: &strategyv1.IntentProvenance{
			Reason:          in.Provenance.Reason,
			CorrelationId:   in.Provenance.CorrelationID,
			TraceId:         in.Provenance.TraceID,
			ParentSignalIds: parentSignalIDs,
			PolicyHash:      in.Provenance.PolicyHash,
		},
	}
}

func ProtoToDomainStrategyIntentV1(in *strategyv1.StrategyIntentV1) strategydomain.StrategyIntentV1 {
	if in == nil {
		return strategydomain.StrategyIntentV1{}
	}
	strategyRef := in.GetStrategy()
	scope := in.GetScope()
	sizing := in.GetSizing()
	constraints := in.GetConstraints()
	provenance := in.GetProvenance()

	return strategydomain.StrategyIntentV1{
		IntentID: in.GetIntentId(),
		Strategy: strategydomain.StrategyRef{
			StrategyID:         strategyRef.GetStrategyId(),
			StrategyVersion:    strategyRef.GetStrategyVersion(),
			StrategyInstanceID: strategyRef.GetStrategyInstanceId(),
		},
		Scope: strategydomain.IntentScope{
			Venue:     scope.GetVenue(),
			Symbol:    scope.GetSymbol(),
			AccountID: scope.GetAccountId(),
		},
		Side: sideProtoToDomain(in.GetSide()),
		Sizing: strategydomain.SizingIntent{
			Mode:           sizingModeProtoToDomain(sizing.GetMode()),
			Value:          sizing.GetValue(),
			MaxNotionalUSD: sizing.GetMaxNotionalUsd(),
		},
		Constraints: strategydomain.ExecutionConstraints{
			OrderType:      orderTypeProtoToDomain(constraints.GetOrderType()),
			TimeInForce:    tifProtoToDomain(constraints.GetTimeInForce()),
			LimitPrice:     constraints.GetLimitPrice(),
			MaxSlippageBps: constraints.GetMaxSlippageBps(),
			PostOnly:       constraints.GetPostOnly(),
			ReduceOnly:     constraints.GetReduceOnly(),
		},
		CreatedAtMs: in.GetCreatedAtMs(),
		ExpiresAtMs: in.GetExpiresAtMs(),
		Provenance: strategydomain.IntentProvenance{
			Reason:          provenance.GetReason(),
			CorrelationID:   provenance.GetCorrelationId(),
			TraceID:         provenance.GetTraceId(),
			ParentSignalIDs: append([]string(nil), provenance.GetParentSignalIds()...),
			PolicyHash:      provenance.GetPolicyHash(),
		},
	}
}

func DomainToProtoExecutionEventV1(in executiondomain.ExecutionEventV1) *executionv1.ExecutionEventV1 {
	return &executionv1.ExecutionEventV1{
		EventId: executionEventID(in.EventID),
		Status:  statusDomainToProto(in.Status),
		Correlation: &executionv1.ExecutionCorrelation{
			IntentId:      in.Correlation.IntentID,
			OrderId:       in.Correlation.OrderID,
			VenueOrderId:  in.Correlation.VenueOrderID,
			ClientOrderId: in.Correlation.ClientOrderID,
			Venue:         in.Correlation.Venue,
			Symbol:        in.Correlation.Symbol,
			AccountId:     in.Correlation.AccountID,
		},
		TsEventMs:           in.TsEventMs,
		TsExchangeMs:        in.TsExchangeMs,
		ExecutionSeq:        in.ExecutionSeq,
		Attempt:             in.Attempt,
		RequestedQty:        in.RequestedQty,
		CumulativeFilledQty: in.CumulativeFilledQty,
		LastFillQty:         in.LastFillQty,
		LeavesQty:           in.LeavesQty,
		LimitPrice:          in.LimitPrice,
		AvgFillPrice:        in.AvgFillPrice,
		LastFillPrice:       in.LastFillPrice,
		Reason:              in.Reason,
		Provenance: &executionv1.ExecutionProvenance{
			CorrelationId: in.Provenance.CorrelationID,
			TraceId:       in.Provenance.TraceID,
			Source:        in.Provenance.Source,
		},
	}
}

func ProtoToDomainExecutionEventV1(in *executionv1.ExecutionEventV1) executiondomain.ExecutionEventV1 {
	if in == nil {
		return executiondomain.ExecutionEventV1{}
	}
	correlation := in.GetCorrelation()
	provenance := in.GetProvenance()
	return executiondomain.ExecutionEventV1{
		EventID: executionEventID(in.GetEventId()),
		Status:  statusProtoToDomain(in.GetStatus()),
		Correlation: executiondomain.ExecutionCorrelation{
			IntentID:      correlation.GetIntentId(),
			OrderID:       correlation.GetOrderId(),
			VenueOrderID:  correlation.GetVenueOrderId(),
			ClientOrderID: correlation.GetClientOrderId(),
			Venue:         correlation.GetVenue(),
			Symbol:        correlation.GetSymbol(),
			AccountID:     correlation.GetAccountId(),
		},
		TsEventMs:           in.GetTsEventMs(),
		TsExchangeMs:        in.GetTsExchangeMs(),
		ExecutionSeq:        in.GetExecutionSeq(),
		Attempt:             in.GetAttempt(),
		RequestedQty:        in.GetRequestedQty(),
		CumulativeFilledQty: in.GetCumulativeFilledQty(),
		LastFillQty:         in.GetLastFillQty(),
		LeavesQty:           in.GetLeavesQty(),
		LimitPrice:          in.GetLimitPrice(),
		AvgFillPrice:        in.GetAvgFillPrice(),
		LastFillPrice:       in.GetLastFillPrice(),
		Reason:              in.GetReason(),
		Provenance: executiondomain.ExecutionProvenance{
			CorrelationID: provenance.GetCorrelationId(),
			TraceID:       provenance.GetTraceId(),
			Source:        provenance.GetSource(),
		},
	}
}

func DomainToProtoPortfolioStateV1(in portfoliodomain.PortfolioStateV1) *portfoliov1.PortfolioStateV1 {
	balances := make([]*portfoliov1.BalanceV1, len(in.Balances))
	for i, b := range in.Balances {
		balances[i] = &portfoliov1.BalanceV1{Asset: b.Asset, Total: b.Total, Available: b.Available, Locked: b.Locked}
	}
	positions := make([]*portfoliov1.PositionV1, len(in.Positions))
	for i, p := range in.Positions {
		positions[i] = &portfoliov1.PositionV1{
			Venue:         p.Venue,
			Symbol:        p.Symbol,
			Quantity:      p.Quantity,
			AvgEntryPrice: p.AvgEntryPrice,
			NotionalUsd:   p.NotionalUSD,
			RealizedPnl:   p.RealizedPnL,
			UnrealizedPnl: p.UnrealizedPnL,
		}
	}
	exposures := make([]*portfoliov1.ExposureV1, len(in.Exposures))
	for i, e := range in.Exposures {
		exposures[i] = &portfoliov1.ExposureV1{
			Symbol:           e.Symbol,
			NetQty:           e.NetQty,
			GrossNotionalUsd: e.GrossNotionalUSD,
			Leverage:         e.Leverage,
		}
	}
	return &portfoliov1.PortfolioStateV1{
		StateId:          in.StateID,
		Scope:            scopeDomainToProto(in.Scope),
		AccountId:        in.AccountID,
		Venue:            in.Venue,
		ProjectedAtMs:    in.ProjectedAtMs,
		Balances:         balances,
		Positions:        positions,
		Exposures:        exposures,
		EquityUsd:        in.EquityUSD,
		RealizedPnlUsd:   in.RealizedPnlUSD,
		UnrealizedPnlUsd: in.UnrealizedPnlUSD,
		Risk: &portfoliov1.RiskSnapshotV1{
			MarginUsedUsd:        in.Risk.MarginUsedUSD,
			MarginAvailableUsd:   in.Risk.MarginAvailableUSD,
			MaintenanceMarginUsd: in.Risk.MaintenanceMarginUSD,
			Var_95Usd:            in.Risk.Var95USD,
		},
		Provenance: &portfoliov1.ProjectionProvenanceV1{
			SourceExecutionEventId: in.Provenance.SourceExecutionEventID,
			SourceExecutionSeq:     in.Provenance.SourceExecutionSeq,
			CorrelationId:          in.Provenance.CorrelationID,
			TraceId:                in.Provenance.TraceID,
			ProjectorVersion:       in.Provenance.ProjectorVersion,
		},
	}
}

func ProtoToDomainPortfolioStateV1(in *portfoliov1.PortfolioStateV1) portfoliodomain.PortfolioStateV1 {
	if in == nil {
		return portfoliodomain.PortfolioStateV1{}
	}
	balances := make([]portfoliodomain.BalanceV1, len(in.GetBalances()))
	for i, b := range in.GetBalances() {
		balances[i] = portfoliodomain.BalanceV1{Asset: b.GetAsset(), Total: b.GetTotal(), Available: b.GetAvailable(), Locked: b.GetLocked()}
	}
	positions := make([]portfoliodomain.PositionV1, len(in.GetPositions()))
	for i, p := range in.GetPositions() {
		positions[i] = portfoliodomain.PositionV1{
			Venue:         p.GetVenue(),
			Symbol:        p.GetSymbol(),
			Quantity:      p.GetQuantity(),
			AvgEntryPrice: p.GetAvgEntryPrice(),
			NotionalUSD:   p.GetNotionalUsd(),
			RealizedPnL:   p.GetRealizedPnl(),
			UnrealizedPnL: p.GetUnrealizedPnl(),
		}
	}
	exposures := make([]portfoliodomain.ExposureV1, len(in.GetExposures()))
	for i, e := range in.GetExposures() {
		exposures[i] = portfoliodomain.ExposureV1{
			Symbol:           e.GetSymbol(),
			NetQty:           e.GetNetQty(),
			GrossNotionalUSD: e.GetGrossNotionalUsd(),
			Leverage:         e.GetLeverage(),
		}
	}
	risk := in.GetRisk()
	provenance := in.GetProvenance()
	return portfoliodomain.PortfolioStateV1{
		StateID:          in.GetStateId(),
		Scope:            scopeProtoToDomain(in.GetScope()),
		AccountID:        in.GetAccountId(),
		Venue:            in.GetVenue(),
		ProjectedAtMs:    in.GetProjectedAtMs(),
		Balances:         balances,
		Positions:        positions,
		Exposures:        exposures,
		EquityUSD:        in.GetEquityUsd(),
		RealizedPnlUSD:   in.GetRealizedPnlUsd(),
		UnrealizedPnlUSD: in.GetUnrealizedPnlUsd(),
		Risk: portfoliodomain.RiskSnapshotV1{
			MarginUsedUSD:        risk.GetMarginUsedUsd(),
			MarginAvailableUSD:   risk.GetMarginAvailableUsd(),
			MaintenanceMarginUSD: risk.GetMaintenanceMarginUsd(),
			Var95USD:             risk.GetVar_95Usd(),
		},
		Provenance: portfoliodomain.ProjectionProvenanceV1{
			SourceExecutionEventID: provenance.GetSourceExecutionEventId(),
			SourceExecutionSeq:     provenance.GetSourceExecutionSeq(),
			CorrelationID:          provenance.GetCorrelationId(),
			TraceID:                provenance.GetTraceId(),
			ProjectorVersion:       provenance.GetProjectorVersion(),
		},
	}
}

func sideDomainToProto(side strategydomain.IntentSide) strategyv1.IntentSide {
	switch side {
	case strategydomain.IntentSideBuy:
		return strategyv1.IntentSide_INTENT_SIDE_BUY
	case strategydomain.IntentSideSell:
		return strategyv1.IntentSide_INTENT_SIDE_SELL
	default:
		return strategyv1.IntentSide_INTENT_SIDE_UNSPECIFIED
	}
}

func sideProtoToDomain(side strategyv1.IntentSide) strategydomain.IntentSide {
	switch side {
	case strategyv1.IntentSide_INTENT_SIDE_BUY:
		return strategydomain.IntentSideBuy
	case strategyv1.IntentSide_INTENT_SIDE_SELL:
		return strategydomain.IntentSideSell
	default:
		return strategydomain.IntentSideUnspecified
	}
}

func sizingModeDomainToProto(mode strategydomain.SizingMode) strategyv1.SizingMode {
	switch mode {
	case strategydomain.SizingModeBaseQuantity:
		return strategyv1.SizingMode_SIZING_MODE_BASE_QUANTITY
	case strategydomain.SizingModeQuoteNotionalUSD:
		return strategyv1.SizingMode_SIZING_MODE_QUOTE_NOTIONAL_USD
	case strategydomain.SizingModeTargetExposurePct:
		return strategyv1.SizingMode_SIZING_MODE_TARGET_EXPOSURE_PCT
	default:
		return strategyv1.SizingMode_SIZING_MODE_UNSPECIFIED
	}
}

func sizingModeProtoToDomain(mode strategyv1.SizingMode) strategydomain.SizingMode {
	switch mode {
	case strategyv1.SizingMode_SIZING_MODE_BASE_QUANTITY:
		return strategydomain.SizingModeBaseQuantity
	case strategyv1.SizingMode_SIZING_MODE_QUOTE_NOTIONAL_USD:
		return strategydomain.SizingModeQuoteNotionalUSD
	case strategyv1.SizingMode_SIZING_MODE_TARGET_EXPOSURE_PCT:
		return strategydomain.SizingModeTargetExposurePct
	default:
		return strategydomain.SizingModeUnspecified
	}
}

func orderTypeDomainToProto(v strategydomain.OrderType) strategyv1.OrderType {
	switch v {
	case strategydomain.OrderTypeMarket:
		return strategyv1.OrderType_ORDER_TYPE_MARKET
	case strategydomain.OrderTypeLimit:
		return strategyv1.OrderType_ORDER_TYPE_LIMIT
	default:
		return strategyv1.OrderType_ORDER_TYPE_UNSPECIFIED
	}
}

func orderTypeProtoToDomain(v strategyv1.OrderType) strategydomain.OrderType {
	switch v {
	case strategyv1.OrderType_ORDER_TYPE_MARKET:
		return strategydomain.OrderTypeMarket
	case strategyv1.OrderType_ORDER_TYPE_LIMIT:
		return strategydomain.OrderTypeLimit
	default:
		return strategydomain.OrderTypeUnspecified
	}
}

func tifDomainToProto(v strategydomain.TimeInForce) strategyv1.TimeInForce {
	switch v {
	case strategydomain.TimeInForceGTC:
		return strategyv1.TimeInForce_TIME_IN_FORCE_GTC
	case strategydomain.TimeInForceIOC:
		return strategyv1.TimeInForce_TIME_IN_FORCE_IOC
	case strategydomain.TimeInForceFOK:
		return strategyv1.TimeInForce_TIME_IN_FORCE_FOK
	default:
		return strategyv1.TimeInForce_TIME_IN_FORCE_UNSPECIFIED
	}
}

func tifProtoToDomain(v strategyv1.TimeInForce) strategydomain.TimeInForce {
	switch v {
	case strategyv1.TimeInForce_TIME_IN_FORCE_GTC:
		return strategydomain.TimeInForceGTC
	case strategyv1.TimeInForce_TIME_IN_FORCE_IOC:
		return strategydomain.TimeInForceIOC
	case strategyv1.TimeInForce_TIME_IN_FORCE_FOK:
		return strategydomain.TimeInForceFOK
	default:
		return strategydomain.TimeInForceUnspecified
	}
}

func statusDomainToProto(v executiondomain.ExecutionStatus) executionv1.ExecutionStatus {
	switch v {
	case executiondomain.ExecutionStatusAccepted:
		return executionv1.ExecutionStatus_EXECUTION_STATUS_ACCEPTED
	case executiondomain.ExecutionStatusRejected:
		return executionv1.ExecutionStatus_EXECUTION_STATUS_REJECTED
	case executiondomain.ExecutionStatusPlaced:
		return executionv1.ExecutionStatus_EXECUTION_STATUS_PLACED
	case executiondomain.ExecutionStatusPartiallyFilled:
		return executionv1.ExecutionStatus_EXECUTION_STATUS_PARTIALLY_FILLED
	case executiondomain.ExecutionStatusFilled:
		return executionv1.ExecutionStatus_EXECUTION_STATUS_FILLED
	case executiondomain.ExecutionStatusCanceled:
		return executionv1.ExecutionStatus_EXECUTION_STATUS_CANCELED
	case executiondomain.ExecutionStatusExpired:
		return executionv1.ExecutionStatus_EXECUTION_STATUS_EXPIRED
	case executiondomain.ExecutionStatusFailed:
		return executionv1.ExecutionStatus_EXECUTION_STATUS_FAILED
	default:
		return executionv1.ExecutionStatus_EXECUTION_STATUS_UNSPECIFIED
	}
}

func statusProtoToDomain(v executionv1.ExecutionStatus) executiondomain.ExecutionStatus {
	switch v {
	case executionv1.ExecutionStatus_EXECUTION_STATUS_ACCEPTED:
		return executiondomain.ExecutionStatusAccepted
	case executionv1.ExecutionStatus_EXECUTION_STATUS_REJECTED:
		return executiondomain.ExecutionStatusRejected
	case executionv1.ExecutionStatus_EXECUTION_STATUS_PLACED:
		return executiondomain.ExecutionStatusPlaced
	case executionv1.ExecutionStatus_EXECUTION_STATUS_PARTIALLY_FILLED:
		return executiondomain.ExecutionStatusPartiallyFilled
	case executionv1.ExecutionStatus_EXECUTION_STATUS_FILLED:
		return executiondomain.ExecutionStatusFilled
	case executionv1.ExecutionStatus_EXECUTION_STATUS_CANCELED:
		return executiondomain.ExecutionStatusCanceled
	case executionv1.ExecutionStatus_EXECUTION_STATUS_EXPIRED:
		return executiondomain.ExecutionStatusExpired
	case executionv1.ExecutionStatus_EXECUTION_STATUS_FAILED:
		return executiondomain.ExecutionStatusFailed
	default:
		return executiondomain.ExecutionStatusUnspecified
	}
}

func scopeDomainToProto(v portfoliodomain.PortfolioScope) portfoliov1.PortfolioScope {
	switch v {
	case portfoliodomain.PortfolioScopeGlobal:
		return portfoliov1.PortfolioScope_PORTFOLIO_SCOPE_GLOBAL
	case portfoliodomain.PortfolioScopeAccount:
		return portfoliov1.PortfolioScope_PORTFOLIO_SCOPE_ACCOUNT
	case portfoliodomain.PortfolioScopeVenueAccount:
		return portfoliov1.PortfolioScope_PORTFOLIO_SCOPE_VENUE_ACCOUNT
	default:
		return portfoliov1.PortfolioScope_PORTFOLIO_SCOPE_UNSPECIFIED
	}
}

func scopeProtoToDomain(v portfoliov1.PortfolioScope) portfoliodomain.PortfolioScope {
	switch v {
	case portfoliov1.PortfolioScope_PORTFOLIO_SCOPE_GLOBAL:
		return portfoliodomain.PortfolioScopeGlobal
	case portfoliov1.PortfolioScope_PORTFOLIO_SCOPE_ACCOUNT:
		return portfoliodomain.PortfolioScopeAccount
	case portfoliov1.PortfolioScope_PORTFOLIO_SCOPE_VENUE_ACCOUNT:
		return portfoliodomain.PortfolioScopeVenueAccount
	default:
		return portfoliodomain.PortfolioScopeUnspecified
	}
}

func executionEventID(v string) string {
	return v
}
