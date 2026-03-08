package contracts

import (
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	portfoliov1 "github.com/market-raccoon/internal/shared/proto/gen/portfolio/v1"
)

// --- AccountSnapshotV1 ---

func DomainToProtoAccountSnapshotV1(in portfoliodomain.AccountSnapshotV1) *portfoliov1.AccountSnapshotV1 {
	venues := make([]*portfoliov1.VenuePositionV1, len(in.Venues))
	for i, v := range in.Venues {
		venues[i] = domainToProtoVenuePositionV1(v)
	}
	return &portfoliov1.AccountSnapshotV1{
		SnapshotId:         in.SnapshotID,
		AccountId:          in.AccountID,
		ProjectedAtMs:      in.ProjectedAtMs,
		Venues:             venues,
		TotalEquityUsd:     in.TotalEquityUSD,
		TotalRealizedUsd:   in.TotalRealizedUSD,
		TotalUnrealizedUsd: in.TotalUnrealized,
		TotalMarginUsedUsd: in.TotalMarginUsed,
		TotalLeverage:      in.TotalLeverage,
		FillSummary:        domainToProtoFillSummaryV1(in.FillSummary),
	}
}

func ProtoToDomainAccountSnapshotV1(in *portfoliov1.AccountSnapshotV1) portfoliodomain.AccountSnapshotV1 {
	if in == nil {
		return portfoliodomain.AccountSnapshotV1{}
	}
	venues := make([]portfoliodomain.VenuePositionV1, len(in.GetVenues()))
	for i, v := range in.GetVenues() {
		venues[i] = protoToDomainVenuePositionV1(v)
	}
	fs := in.GetFillSummary()
	return portfoliodomain.AccountSnapshotV1{
		SnapshotID:       in.GetSnapshotId(),
		AccountID:        in.GetAccountId(),
		ProjectedAtMs:    in.GetProjectedAtMs(),
		Venues:           venues,
		TotalEquityUSD:   in.GetTotalEquityUsd(),
		TotalRealizedUSD: in.GetTotalRealizedUsd(),
		TotalUnrealized:  in.GetTotalUnrealizedUsd(),
		TotalMarginUsed:  in.GetTotalMarginUsedUsd(),
		TotalLeverage:    in.GetTotalLeverage(),
		FillSummary:      protoToDomainFillSummaryV1(fs),
	}
}

// --- PortfolioSummaryV1 ---

func DomainToProtoPortfolioSummaryV1(in portfoliodomain.PortfolioSummaryV1) *portfoliov1.PortfolioSummaryV1 {
	accounts := make([]*portfoliov1.AccountSummaryV1, len(in.Accounts))
	for i, a := range in.Accounts {
		accounts[i] = &portfoliov1.AccountSummaryV1{
			AccountId:        a.AccountID,
			VenueCount:       a.VenueCount,
			PositionCount:    a.PositionCount,
			EquityUsd:        a.EquityUSD,
			RealizedPnlUsd:   a.RealizedPnlUSD,
			UnrealizedPnlUsd: a.UnrealizedPnlUSD,
		}
	}
	return &portfoliov1.PortfolioSummaryV1{
		SummaryId:           in.SummaryID,
		ProjectedAtMs:       in.ProjectedAtMs,
		Accounts:            accounts,
		GlobalEquityUsd:     in.GlobalEquityUSD,
		GlobalRealizedUsd:   in.GlobalRealizedUSD,
		GlobalUnrealizedUsd: in.GlobalUnrealized,
		GlobalMarginUsedUsd: in.GlobalMarginUsed,
		GlobalLeverage:      in.GlobalLeverage,
		TotalPositionCount:  in.TotalPositionCount,
		TotalOpenOrders:     in.TotalOpenOrders,
		FillSummary:         domainToProtoFillSummaryV1(in.FillSummary),
	}
}

func ProtoToDomainPortfolioSummaryV1(in *portfoliov1.PortfolioSummaryV1) portfoliodomain.PortfolioSummaryV1 {
	if in == nil {
		return portfoliodomain.PortfolioSummaryV1{}
	}
	accounts := make([]portfoliodomain.AccountSummaryV1, len(in.GetAccounts()))
	for i, a := range in.GetAccounts() {
		accounts[i] = portfoliodomain.AccountSummaryV1{
			AccountID:        a.GetAccountId(),
			VenueCount:       a.GetVenueCount(),
			PositionCount:    a.GetPositionCount(),
			EquityUSD:        a.GetEquityUsd(),
			RealizedPnlUSD:   a.GetRealizedPnlUsd(),
			UnrealizedPnlUSD: a.GetUnrealizedPnlUsd(),
		}
	}
	fs := in.GetFillSummary()
	return portfoliodomain.PortfolioSummaryV1{
		SummaryID:          in.GetSummaryId(),
		ProjectedAtMs:      in.GetProjectedAtMs(),
		Accounts:           accounts,
		GlobalEquityUSD:    in.GetGlobalEquityUsd(),
		GlobalRealizedUSD:  in.GetGlobalRealizedUsd(),
		GlobalUnrealized:   in.GetGlobalUnrealizedUsd(),
		GlobalMarginUsed:   in.GetGlobalMarginUsedUsd(),
		GlobalLeverage:     in.GetGlobalLeverage(),
		TotalPositionCount: in.GetTotalPositionCount(),
		TotalOpenOrders:    in.GetTotalOpenOrders(),
		FillSummary:        protoToDomainFillSummaryV1(fs),
	}
}

// --- shared helpers ---

func domainToProtoVenuePositionV1(in portfoliodomain.VenuePositionV1) *portfoliov1.VenuePositionV1 {
	positions := make([]*portfoliov1.PositionV1, len(in.Positions))
	for i, p := range in.Positions {
		positions[i] = &portfoliov1.PositionV1{
			Venue:           p.Venue,
			Symbol:          p.Symbol,
			Quantity:        p.Quantity,
			AvgEntryPrice:   p.AvgEntryPrice,
			NotionalUsd:     p.NotionalUSD,
			RealizedPnl:     p.RealizedPnL,
			UnrealizedPnl:   p.UnrealizedPnL,
			TradeCount:      p.TradeCount,
			VolumeTradedUsd: p.VolumeTradedUSD,
			LastFillMs:      p.LastFillMs,
			Side:            p.Side,
		}
	}
	balances := make([]*portfoliov1.BalanceV1, len(in.Balances))
	for i, b := range in.Balances {
		balances[i] = &portfoliov1.BalanceV1{Asset: b.Asset, Total: b.Total, Available: b.Available, Locked: b.Locked}
	}
	return &portfoliov1.VenuePositionV1{
		Venue:            in.Venue,
		Positions:        positions,
		Balances:         balances,
		EquityUsd:        in.EquityUSD,
		RealizedPnlUsd:   in.RealizedPnlUSD,
		UnrealizedPnlUsd: in.UnrealizedPnlUSD,
		MarginUsedUsd:    in.MarginUsedUSD,
	}
}

func protoToDomainVenuePositionV1(in *portfoliov1.VenuePositionV1) portfoliodomain.VenuePositionV1 {
	if in == nil {
		return portfoliodomain.VenuePositionV1{}
	}
	positions := make([]portfoliodomain.PositionV1, len(in.GetPositions()))
	for i, p := range in.GetPositions() {
		positions[i] = portfoliodomain.PositionV1{
			Venue:           p.GetVenue(),
			Symbol:          p.GetSymbol(),
			Quantity:        p.GetQuantity(),
			AvgEntryPrice:   p.GetAvgEntryPrice(),
			NotionalUSD:     p.GetNotionalUsd(),
			RealizedPnL:     p.GetRealizedPnl(),
			UnrealizedPnL:   p.GetUnrealizedPnl(),
			TradeCount:      p.GetTradeCount(),
			VolumeTradedUSD: p.GetVolumeTradedUsd(),
			LastFillMs:      p.GetLastFillMs(),
			Side:            p.GetSide(),
		}
	}
	balances := make([]portfoliodomain.BalanceV1, len(in.GetBalances()))
	for i, b := range in.GetBalances() {
		balances[i] = portfoliodomain.BalanceV1{Asset: b.GetAsset(), Total: b.GetTotal(), Available: b.GetAvailable(), Locked: b.GetLocked()}
	}
	return portfoliodomain.VenuePositionV1{
		Venue:            in.GetVenue(),
		Positions:        positions,
		Balances:         balances,
		EquityUSD:        in.GetEquityUsd(),
		RealizedPnlUSD:   in.GetRealizedPnlUsd(),
		UnrealizedPnlUSD: in.GetUnrealizedPnlUsd(),
		MarginUsedUSD:    in.GetMarginUsedUsd(),
	}
}

func domainToProtoFillSummaryV1(in portfoliodomain.FillSummaryV1) *portfoliov1.FillSummaryV1 {
	return &portfoliov1.FillSummaryV1{
		TotalTradeCount:      in.TotalTradeCount,
		TotalVolumeTradedUsd: in.TotalVolumeTradedUSD,
		WinCount:             in.WinCount,
		LossCount:            in.LossCount,
		LargestWinUsd:        in.LargestWinUSD,
		LargestLossUsd:       in.LargestLossUSD,
		TurnoverUsd:          in.TurnoverUSD,
	}
}

func protoToDomainFillSummaryV1(in *portfoliov1.FillSummaryV1) portfoliodomain.FillSummaryV1 {
	if in == nil {
		return portfoliodomain.FillSummaryV1{}
	}
	return portfoliodomain.FillSummaryV1{
		TotalTradeCount:      in.GetTotalTradeCount(),
		TotalVolumeTradedUSD: in.GetTotalVolumeTradedUsd(),
		WinCount:             in.GetWinCount(),
		LossCount:            in.GetLossCount(),
		LargestWinUSD:        in.GetLargestWinUsd(),
		LargestLossUSD:       in.GetLargestLossUsd(),
		TurnoverUSD:          in.GetTurnoverUsd(),
	}
}

// --- Query converters ---

func DomainToProtoPortfolioStateQueryRequest(in portfoliodomain.PortfolioStateQuery) *portfoliov1.PortfolioStateQueryRequest {
	return &portfoliov1.PortfolioStateQueryRequest{
		AccountId: in.AccountID,
		Venue:     in.Venue,
		Symbol:    in.Symbol,
		Limit:     in.Limit,
	}
}

func ProtoToDomainPortfolioStateQueryRequest(in *portfoliov1.PortfolioStateQueryRequest) portfoliodomain.PortfolioStateQuery {
	if in == nil {
		return portfoliodomain.PortfolioStateQuery{}
	}
	return portfoliodomain.PortfolioStateQuery{
		AccountID: in.GetAccountId(),
		Venue:     in.GetVenue(),
		Symbol:    in.GetSymbol(),
		Limit:     in.GetLimit(),
	}
}

func DomainToProtoAccountSnapshotQueryRequest(in portfoliodomain.AccountSnapshotQuery) *portfoliov1.AccountSnapshotQueryRequest {
	return &portfoliov1.AccountSnapshotQueryRequest{
		AccountId: in.AccountID,
		FromMs:    in.FromMs,
		ToMs:      in.ToMs,
		Limit:     in.Limit,
	}
}

func ProtoToDomainAccountSnapshotQueryRequest(in *portfoliov1.AccountSnapshotQueryRequest) portfoliodomain.AccountSnapshotQuery {
	if in == nil {
		return portfoliodomain.AccountSnapshotQuery{}
	}
	return portfoliodomain.AccountSnapshotQuery{
		AccountID: in.GetAccountId(),
		FromMs:    in.GetFromMs(),
		ToMs:      in.GetToMs(),
		Limit:     in.GetLimit(),
	}
}

func DomainToProtoPortfolioSummaryQueryRequest(in portfoliodomain.PortfolioSummaryQuery) *portfoliov1.PortfolioSummaryQueryRequest {
	return &portfoliov1.PortfolioSummaryQueryRequest{
		FromMs: in.FromMs,
		ToMs:   in.ToMs,
		Limit:  in.Limit,
	}
}

func ProtoToDomainPortfolioSummaryQueryRequest(in *portfoliov1.PortfolioSummaryQueryRequest) portfoliodomain.PortfolioSummaryQuery {
	if in == nil {
		return portfoliodomain.PortfolioSummaryQuery{}
	}
	return portfoliodomain.PortfolioSummaryQuery{
		FromMs: in.GetFromMs(),
		ToMs:   in.GetToMs(),
		Limit:  in.GetLimit(),
	}
}
