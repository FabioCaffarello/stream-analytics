package app

import (
	"math"
	"testing"

	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
)

func mkFillEvent(eventID, venue, symbol, accountID string, qty, price float64, seq int64) executiondomain.ExecutionEventV1 {
	return executiondomain.ExecutionEventV1{
		EventID:             eventID,
		Status:              executiondomain.ExecutionStatusFilled,
		Correlation:         executiondomain.ExecutionCorrelation{IntentID: "i-" + eventID, OrderID: "o-" + eventID, Venue: venue, Symbol: symbol, AccountID: accountID},
		TsEventMs:           1_700_000_000_000 + seq*1000,
		ExecutionSeq:        seq,
		Attempt:             1,
		RequestedQty:        qty,
		CumulativeFilledQty: math.Abs(qty),
		LastFillQty:         qty,
		LeavesQty:           0,
		AvgFillPrice:        price,
		LastFillPrice:       price,
		Provenance:          executiondomain.ExecutionProvenance{CorrelationID: "c-" + eventID, Source: "executor.bootstrap.v1"},
	}
}

func TestSnapshotStates_Deterministic(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	p.Apply(mkFillEvent("e1", "binance", "BTCUSDT", "paper", 1, 100, 1))
	p.Apply(mkFillEvent("e2", "bybit", "ETHUSDT", "paper", 2, 50, 2))

	states := p.SnapshotStates()
	if len(states) != 2 {
		t.Fatalf("states=%d want=2", len(states))
	}
	// Should be sorted by key
	if states[0].Venue != "binance" && states[1].Venue != "bybit" {
		t.Fatal("states not sorted by key")
	}
}

func TestSnapshotStates_FillMetrics(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	p.Apply(mkFillEvent("e1", "binance", "BTCUSDT", "paper", 1, 100, 1))

	states := p.SnapshotStates()
	if len(states) != 1 {
		t.Fatalf("states=%d want=1", len(states))
	}
	pos := states[0].Positions[0]
	if pos.TradeCount != 1 {
		t.Fatalf("trade_count=%d want=1", pos.TradeCount)
	}
	if pos.VolumeTradedUSD != 100 {
		t.Fatalf("volume_traded=%v want=100", pos.VolumeTradedUSD)
	}
	if pos.Side != "long" {
		t.Fatalf("side=%q want=long", pos.Side)
	}
}

func TestBuildAccountSnapshot_SingleVenue(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	p.Apply(mkFillEvent("e1", "binance", "BTCUSDT", "paper", 1, 100, 1))
	p.Apply(mkFillEvent("e2", "binance", "ETHUSDT", "paper", 5, 50, 2))

	snap, ok := p.BuildAccountSnapshot("paper", 1_700_000_010_000)
	if !ok {
		t.Fatal("snapshot not built")
	}
	if snap.AccountID != "paper" {
		t.Fatalf("account=%q want=paper", snap.AccountID)
	}
	if len(snap.Venues) != 1 {
		t.Fatalf("venues=%d want=1", len(snap.Venues))
	}
	if len(snap.Venues[0].Positions) != 2 {
		t.Fatalf("positions=%d want=2", len(snap.Venues[0].Positions))
	}
	if snap.FillSummary.TotalTradeCount != 2 {
		t.Fatalf("total_trades=%d want=2", snap.FillSummary.TotalTradeCount)
	}
	if p := snap.Validate(); p != nil {
		t.Fatalf("validation error: %v", p.Message)
	}
}

func TestBuildAccountSnapshot_MultiVenue(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	p.Apply(mkFillEvent("e1", "binance", "BTCUSDT", "paper", 1, 100, 1))
	p.Apply(mkFillEvent("e2", "bybit", "BTCUSDT", "paper", 2, 100, 2))

	snap, ok := p.BuildAccountSnapshot("paper", 1_700_000_010_000)
	if !ok {
		t.Fatal("snapshot not built")
	}
	if len(snap.Venues) != 2 {
		t.Fatalf("venues=%d want=2", len(snap.Venues))
	}
	// Venues should be sorted
	if snap.Venues[0].Venue != "binance" || snap.Venues[1].Venue != "bybit" {
		t.Fatalf("venues not sorted: %q, %q", snap.Venues[0].Venue, snap.Venues[1].Venue)
	}
}

func TestBuildAccountSnapshot_EmptyAccount(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	p.Apply(mkFillEvent("e1", "binance", "BTCUSDT", "paper", 1, 100, 1))

	_, ok := p.BuildAccountSnapshot("other", 1_700_000_010_000)
	if ok {
		t.Fatal("expected no snapshot for unknown account")
	}
}

func TestBuildAccountSnapshot_BlankAccountID(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	_, ok := p.BuildAccountSnapshot("", 1_700_000_010_000)
	if ok {
		t.Fatal("expected no snapshot for blank account_id")
	}
}

func TestBuildPortfolioSummary_MultiAccount(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	p.Apply(mkFillEvent("e1", "binance", "BTCUSDT", "paper", 1, 100, 1))
	p.Apply(mkFillEvent("e2", "bybit", "BTCUSDT", "live", 2, 100, 2))

	sum, ok := p.BuildPortfolioSummary(1_700_000_010_000)
	if !ok {
		t.Fatal("summary not built")
	}
	if len(sum.Accounts) != 2 {
		t.Fatalf("accounts=%d want=2", len(sum.Accounts))
	}
	if sum.TotalPositionCount != 2 {
		t.Fatalf("total_positions=%d want=2", sum.TotalPositionCount)
	}
	if sum.FillSummary.TotalTradeCount != 2 {
		t.Fatalf("total_trades=%d want=2", sum.FillSummary.TotalTradeCount)
	}
	if p := sum.Validate(); p != nil {
		t.Fatalf("validation error: %v", p.Message)
	}
}

func TestBuildPortfolioSummary_Empty(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	_, ok := p.BuildPortfolioSummary(1_700_000_010_000)
	if ok {
		t.Fatal("expected no summary for empty projector")
	}
}

func TestBuildPortfolioSummary_VenueCountPerAccount(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	p.Apply(mkFillEvent("e1", "binance", "BTCUSDT", "paper", 1, 100, 1))
	p.Apply(mkFillEvent("e2", "bybit", "BTCUSDT", "paper", 1, 100, 2))
	p.Apply(mkFillEvent("e3", "binance", "ETHUSDT", "paper", 1, 50, 3))

	sum, ok := p.BuildPortfolioSummary(1_700_000_010_000)
	if !ok {
		t.Fatal("summary not built")
	}
	if len(sum.Accounts) != 1 {
		t.Fatalf("accounts=%d want=1", len(sum.Accounts))
	}
	if sum.Accounts[0].VenueCount != 2 {
		t.Fatalf("venue_count=%d want=2", sum.Accounts[0].VenueCount)
	}
	if sum.Accounts[0].PositionCount != 3 {
		t.Fatalf("position_count=%d want=3", sum.Accounts[0].PositionCount)
	}
}

func TestFillMetrics_WinLossTracking(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	// Buy 1 BTC at 100
	p.Apply(mkFillEvent("e1", "binance", "BTCUSDT", "paper", 1, 100, 1))
	// Sell 1 BTC at 120 → win of 20
	p.Apply(mkFillEvent("e2", "binance", "BTCUSDT", "paper", -1, 120, 2))

	states := p.SnapshotStates()
	if len(states) != 1 {
		t.Fatalf("states=%d want=1", len(states))
	}
	fs := states[0].FillSummary
	if fs.TotalTradeCount != 2 {
		t.Fatalf("trade_count=%d want=2", fs.TotalTradeCount)
	}
	if fs.WinCount != 1 {
		t.Fatalf("win_count=%d want=1", fs.WinCount)
	}
	if fs.LossCount != 0 {
		t.Fatalf("loss_count=%d want=0", fs.LossCount)
	}
	if math.Abs(fs.LargestWinUSD-20) > 0.01 {
		t.Fatalf("largest_win=%v want=20", fs.LargestWinUSD)
	}
}

func TestFillMetrics_ShortSideLoss(t *testing.T) {
	p := NewBootstrapProjector(DefaultProjectorConfig())
	// Short 1 BTC at 100
	p.Apply(mkFillEvent("e1", "binance", "BTCUSDT", "paper", -1, 100, 1))
	// Cover at 110 → loss of 10
	p.Apply(mkFillEvent("e2", "binance", "BTCUSDT", "paper", 1, 110, 2))

	states := p.SnapshotStates()
	fs := states[0].FillSummary
	if fs.LossCount != 1 {
		t.Fatalf("loss_count=%d want=1", fs.LossCount)
	}
	if math.Abs(fs.LargestLossUSD-(-10)) > 0.01 {
		t.Fatalf("largest_loss=%v want=-10", fs.LargestLossUSD)
	}
	if states[0].Positions[0].Side != "" {
		t.Fatalf("side=%q want empty (flat)", states[0].Positions[0].Side)
	}
}
