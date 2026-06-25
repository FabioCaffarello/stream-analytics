package app

import (
	"math"
	"sort"
	"strings"

	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
)

// SnapshotStates returns all currently accumulated venue-scoped portfolio states.
// The returned slice is deterministically ordered by portfolio key.
func (p *BootstrapProjector) SnapshotStates() []portfoliodomain.PortfolioStateV1 {
	keys := make([]string, 0, len(p.states))
	for k := range p.states {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]portfoliodomain.PortfolioStateV1, 0, len(keys))
	for _, k := range keys {
		ps := p.states[k]
		parts := splitPortfolioKey(k)
		if parts == nil {
			continue
		}
		accountID, venue, symbol := parts[0], parts[1], parts[2]
		baseAsset, quoteAsset := splitInstrument(symbol)

		lockedBase, lockedQuote := lockedBalances(ps)
		notional := math.Abs(ps.qty) * ps.lastPrice
		unrealized := unrealizedPnL(ps.qty, ps.avgPrice, ps.lastPrice)
		equity := ps.cashUSD + (ps.qty * ps.lastPrice)
		marginUsed := (notional * 0.1) + (lockedQuote * 0.05) + (lockedBase * ps.lastPrice * 0.05)
		if marginUsed < 0 {
			marginUsed = 0
		}
		marginAvailable := equity - marginUsed
		if marginAvailable < 0 {
			marginAvailable = 0
		}
		leverage := 0.0
		if math.Abs(equity) > 1e-9 {
			leverage = (notional + lockedQuote + (lockedBase * ps.lastPrice)) / math.Abs(equity)
		}

		side := ""
		if ps.qty > 1e-9 {
			side = "long"
		} else if ps.qty < -1e-9 {
			side = "short"
		}

		state := portfoliodomain.PortfolioStateV1{
			StateID:   sharedhash.HashFieldsFast("portfolio-snapshot", k),
			Scope:     portfoliodomain.PortfolioScopeVenueAccount,
			AccountID: accountID,
			Venue:     venue,
			Balances: []portfoliodomain.BalanceV1{
				{Asset: baseAsset, Total: ps.qty, Available: ps.qty - lockedBase, Locked: lockedBase},
				{Asset: quoteAsset, Total: ps.cashUSD, Available: ps.cashUSD - lockedQuote, Locked: lockedQuote},
			},
			Positions: []portfoliodomain.PositionV1{
				{
					Venue:           venue,
					Symbol:          symbol,
					Quantity:        ps.qty,
					AvgEntryPrice:   ps.avgPrice,
					NotionalUSD:     notional,
					RealizedPnL:     ps.realized,
					UnrealizedPnL:   unrealized,
					TradeCount:      ps.tradeCount,
					VolumeTradedUSD: ps.volumeTradedUSD,
					LastFillMs:      ps.lastFillMs,
					Side:            side,
				},
			},
			Exposures: []portfoliodomain.ExposureV1{
				{
					Symbol:           symbol,
					NetQty:           ps.qty,
					GrossNotionalUSD: notional + lockedQuote + (lockedBase * ps.lastPrice),
					Leverage:         leverage,
				},
			},
			EquityUSD:        equity,
			RealizedPnlUSD:   ps.realized,
			UnrealizedPnlUSD: unrealized,
			Risk: portfoliodomain.RiskSnapshotV1{
				MarginUsedUSD:        marginUsed,
				MarginAvailableUSD:   marginAvailable,
				MaintenanceMarginUSD: marginUsed * 0.5,
				Var95USD:             notional * 0.02,
			},
			FillSummary: portfoliodomain.FillSummaryV1{
				TotalTradeCount:      ps.tradeCount,
				TotalVolumeTradedUSD: ps.volumeTradedUSD,
				WinCount:             ps.winCount,
				LossCount:            ps.lossCount,
				LargestWinUSD:        ps.largestWinUSD,
				LargestLossUSD:       ps.largestLossUSD,
				TurnoverUSD:          ps.turnoverUSD,
			},
		}
		result = append(result, state)
	}
	return result
}

// BuildAccountSnapshot aggregates all venue-scoped states for a given account
// into an AccountSnapshotV1 read model.
func (p *BootstrapProjector) BuildAccountSnapshot(accountID string, nowMs int64) (portfoliodomain.AccountSnapshotV1, bool) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return portfoliodomain.AccountSnapshotV1{}, false
	}

	venueMap := make(map[string]*portfoliodomain.VenuePositionV1)
	var totalEquity, totalRealized, totalUnrealized, totalMarginUsed float64
	var totalGrossNotional float64
	var fills portfoliodomain.FillSummaryV1

	for k, ps := range p.states {
		parts := splitPortfolioKey(k)
		if parts == nil || parts[0] != accountID {
			continue
		}
		venue, symbol := parts[1], parts[2]
		baseAsset, quoteAsset := splitInstrument(symbol)
		lockedBase, lockedQuote := lockedBalances(ps)
		notional := math.Abs(ps.qty) * ps.lastPrice
		unrealized := unrealizedPnL(ps.qty, ps.avgPrice, ps.lastPrice)
		equity := ps.cashUSD + (ps.qty * ps.lastPrice)
		marginUsed := (notional * 0.1) + (lockedQuote * 0.05) + (lockedBase * ps.lastPrice * 0.05)
		if marginUsed < 0 {
			marginUsed = 0
		}

		side := ""
		if ps.qty > 1e-9 {
			side = "long"
		} else if ps.qty < -1e-9 {
			side = "short"
		}

		vp, ok := venueMap[venue]
		if !ok {
			vp = &portfoliodomain.VenuePositionV1{Venue: venue}
			venueMap[venue] = vp
		}
		vp.Positions = append(vp.Positions, portfoliodomain.PositionV1{
			Venue: venue, Symbol: symbol, Quantity: ps.qty,
			AvgEntryPrice: ps.avgPrice, NotionalUSD: notional,
			RealizedPnL: ps.realized, UnrealizedPnL: unrealized,
			TradeCount: ps.tradeCount, VolumeTradedUSD: ps.volumeTradedUSD,
			LastFillMs: ps.lastFillMs, Side: side,
		})
		vp.Balances = append(vp.Balances,
			portfoliodomain.BalanceV1{Asset: baseAsset, Total: ps.qty, Available: ps.qty - lockedBase, Locked: lockedBase},
			portfoliodomain.BalanceV1{Asset: quoteAsset, Total: ps.cashUSD, Available: ps.cashUSD - lockedQuote, Locked: lockedQuote},
		)
		vp.EquityUSD += equity
		vp.RealizedPnlUSD += ps.realized
		vp.UnrealizedPnlUSD += unrealized
		vp.MarginUsedUSD += marginUsed

		totalEquity += equity
		totalRealized += ps.realized
		totalUnrealized += unrealized
		totalMarginUsed += marginUsed
		totalGrossNotional += notional + lockedQuote + (lockedBase * ps.lastPrice)

		fills.TotalTradeCount += ps.tradeCount
		fills.TotalVolumeTradedUSD += ps.volumeTradedUSD
		fills.WinCount += ps.winCount
		fills.LossCount += ps.lossCount
		fills.TurnoverUSD += ps.turnoverUSD
		if ps.largestWinUSD > fills.LargestWinUSD {
			fills.LargestWinUSD = ps.largestWinUSD
		}
		if ps.largestLossUSD < fills.LargestLossUSD {
			fills.LargestLossUSD = ps.largestLossUSD
		}
	}

	if len(venueMap) == 0 {
		return portfoliodomain.AccountSnapshotV1{}, false
	}

	venues := make([]portfoliodomain.VenuePositionV1, 0, len(venueMap))
	venueKeys := make([]string, 0, len(venueMap))
	for k := range venueMap {
		venueKeys = append(venueKeys, k)
	}
	sort.Strings(venueKeys)
	for _, k := range venueKeys {
		venues = append(venues, *venueMap[k])
	}

	totalLeverage := 0.0
	if math.Abs(totalEquity) > 1e-9 {
		totalLeverage = totalGrossNotional / math.Abs(totalEquity)
	}

	snap := portfoliodomain.AccountSnapshotV1{
		SnapshotID:         sharedhash.HashFieldsFast("account-snapshot", accountID),
		AccountID:          accountID,
		ProjectedAtMs:      nowMs,
		Venues:             venues,
		TotalEquityUSD:     totalEquity,
		TotalRealizedUSD:   totalRealized,
		TotalUnrealizedUSD: totalUnrealized,
		TotalMarginUsedUSD: totalMarginUsed,
		TotalLeverage:      totalLeverage,
		FillSummary:        fills,
	}
	return snap, true
}

// BuildPortfolioSummary aggregates all accounts into a global PortfolioSummaryV1.
func (p *BootstrapProjector) BuildPortfolioSummary(nowMs int64) (portfoliodomain.PortfolioSummaryV1, bool) {
	if len(p.states) == 0 {
		return portfoliodomain.PortfolioSummaryV1{}, false
	}

	accountMap := make(map[string]*portfoliodomain.AccountSummaryV1)
	var globalEquity, globalRealized, globalUnrealized, globalMarginUsed float64
	var globalGrossNotional float64
	var totalPositions int32
	var totalOpenOrders int32
	var fills portfoliodomain.FillSummaryV1

	for k, ps := range p.states {
		parts := splitPortfolioKey(k)
		if parts == nil {
			continue
		}
		accountID, venue := parts[0], parts[1]
		_ = venue
		lockedBase, lockedQuote := lockedBalances(ps)
		notional := math.Abs(ps.qty) * ps.lastPrice
		unrealized := unrealizedPnL(ps.qty, ps.avgPrice, ps.lastPrice)
		equity := ps.cashUSD + (ps.qty * ps.lastPrice)
		marginUsed := (notional * 0.1) + (lockedQuote * 0.05) + (lockedBase * ps.lastPrice * 0.05)
		if marginUsed < 0 {
			marginUsed = 0
		}

		as, ok := accountMap[accountID]
		if !ok {
			as = &portfoliodomain.AccountSummaryV1{AccountID: accountID}
			accountMap[accountID] = as
		}
		as.PositionCount++
		as.EquityUSD += equity
		as.RealizedPnlUSD += ps.realized
		as.UnrealizedPnlUSD += unrealized

		globalEquity += equity
		globalRealized += ps.realized
		globalUnrealized += unrealized
		globalMarginUsed += marginUsed
		globalGrossNotional += notional + lockedQuote + (lockedBase * ps.lastPrice)
		totalPositions++
		totalOpenOrders += int32(len(ps.orders))

		fills.TotalTradeCount += ps.tradeCount
		fills.TotalVolumeTradedUSD += ps.volumeTradedUSD
		fills.WinCount += ps.winCount
		fills.LossCount += ps.lossCount
		fills.TurnoverUSD += ps.turnoverUSD
		if ps.largestWinUSD > fills.LargestWinUSD {
			fills.LargestWinUSD = ps.largestWinUSD
		}
		if ps.largestLossUSD < fills.LargestLossUSD {
			fills.LargestLossUSD = ps.largestLossUSD
		}
	}

	// Count unique venues per account
	venuesByAccount := make(map[string]map[string]bool)
	for k := range p.states {
		parts := splitPortfolioKey(k)
		if parts == nil {
			continue
		}
		if venuesByAccount[parts[0]] == nil {
			venuesByAccount[parts[0]] = make(map[string]bool)
		}
		venuesByAccount[parts[0]][parts[1]] = true
	}
	for acct, venues := range venuesByAccount {
		if as, ok := accountMap[acct]; ok {
			as.VenueCount = int32(len(venues))
		}
	}

	accountKeys := make([]string, 0, len(accountMap))
	for k := range accountMap {
		accountKeys = append(accountKeys, k)
	}
	sort.Strings(accountKeys)

	accounts := make([]portfoliodomain.AccountSummaryV1, 0, len(accountKeys))
	for _, k := range accountKeys {
		accounts = append(accounts, *accountMap[k])
	}

	globalLeverage := 0.0
	if math.Abs(globalEquity) > 1e-9 {
		globalLeverage = globalGrossNotional / math.Abs(globalEquity)
	}

	sum := portfoliodomain.PortfolioSummaryV1{
		SummaryID:           sharedhash.HashFieldsFast("portfolio-summary"),
		ProjectedAtMs:       nowMs,
		Accounts:            accounts,
		GlobalEquityUSD:     globalEquity,
		GlobalRealizedUSD:   globalRealized,
		GlobalUnrealizedUSD: globalUnrealized,
		GlobalMarginUsedUSD: globalMarginUsed,
		GlobalLeverage:      globalLeverage,
		TotalPositionCount:  totalPositions,
		TotalOpenOrders:     totalOpenOrders,
		FillSummary:         fills,
	}
	return sum, true
}

func splitPortfolioKey(key string) []string {
	parts := strings.SplitN(key, "|", 3)
	if len(parts) != 3 {
		return nil
	}
	return parts
}
