package domain

import "testing"

func TestAccountSnapshotV1_Validate_Valid(t *testing.T) {
	snap := AccountSnapshotV1{
		SnapshotID:    "snap-1",
		AccountID:     "paper",
		ProjectedAtMs: 1_700_000_001_000,
		Venues: []VenuePositionV1{
			{
				Venue:     "binance",
				Positions: []PositionV1{{Venue: "binance", Symbol: "BTCUSDT", Quantity: 1}},
				Balances:  []BalanceV1{{Asset: "USDT", Total: 10000}},
				EquityUSD: 10000,
			},
		},
		TotalEquityUSD: 10000,
	}
	if p := snap.Validate(); p != nil {
		t.Fatalf("unexpected validation error: %v", p.Message)
	}
}

func TestAccountSnapshotV1_Validate_EmptyID(t *testing.T) {
	snap := AccountSnapshotV1{ProjectedAtMs: 1, Venues: []VenuePositionV1{{}}}
	if p := snap.Validate(); p == nil {
		t.Fatal("expected validation error for empty snapshot_id")
	}
}

func TestAccountSnapshotV1_Validate_EmptyAccount(t *testing.T) {
	snap := AccountSnapshotV1{SnapshotID: "s", ProjectedAtMs: 1, Venues: []VenuePositionV1{{}}}
	if p := snap.Validate(); p == nil {
		t.Fatal("expected validation error for empty account_id")
	}
}

func TestAccountSnapshotV1_Validate_NoVenues(t *testing.T) {
	snap := AccountSnapshotV1{SnapshotID: "s", AccountID: "a", ProjectedAtMs: 1}
	if p := snap.Validate(); p == nil {
		t.Fatal("expected validation error for empty venues")
	}
}

func TestPortfolioSummaryV1_Validate_Valid(t *testing.T) {
	sum := PortfolioSummaryV1{
		SummaryID:     "sum-1",
		ProjectedAtMs: 1_700_000_001_000,
	}
	if p := sum.Validate(); p != nil {
		t.Fatalf("unexpected validation error: %v", p.Message)
	}
}

func TestPortfolioSummaryV1_Validate_EmptyID(t *testing.T) {
	sum := PortfolioSummaryV1{ProjectedAtMs: 1}
	if p := sum.Validate(); p == nil {
		t.Fatal("expected validation error for empty summary_id")
	}
}

func TestPortfolioSummaryV1_Validate_ZeroTimestamp(t *testing.T) {
	sum := PortfolioSummaryV1{SummaryID: "s"}
	if p := sum.Validate(); p == nil {
		t.Fatal("expected validation error for zero timestamp")
	}
}
