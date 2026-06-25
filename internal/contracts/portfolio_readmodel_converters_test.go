package contracts_test

import (
	"reflect"
	"testing"

	"github.com/market-raccoon/internal/contracts"
	portfoliodomain "github.com/market-raccoon/internal/core/portfolio/domain"
	"github.com/market-raccoon/internal/shared/codec"
)

// --- AccountSnapshotV1 round-trip ---

func TestAccountSnapshotV1_DomainProtoRoundTrip(t *testing.T) {
	original := fixtureAccountSnapshot()
	proto := contracts.DomainToProtoAccountSnapshotV1(original)
	roundTripped := contracts.ProtoToDomainAccountSnapshotV1(proto)

	if !reflect.DeepEqual(original, roundTripped) {
		t.Fatalf("round-trip mismatch:\n  original:     %+v\n  roundTripped:  %+v", original, roundTripped)
	}
}

func TestAccountSnapshotV1_NilProtoReturnsZero(t *testing.T) {
	got := contracts.ProtoToDomainAccountSnapshotV1(nil)
	if got.SnapshotID != "" {
		t.Fatal("expected zero value for nil proto")
	}
}

func TestAccountSnapshotV1_CodecRoundTrip(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterPortfolioPayloadV1(reg); p != nil {
		t.Fatalf("register failed: %s", p.Message)
	}

	snap := fixtureAccountSnapshot()
	for _, format := range []codec.Format{codec.FormatJSON, codec.FormatProto} {
		key := codec.SchemaKey{Type: portfoliodomain.AccountSnapshotEventType, Version: 1, Format: format}
		enc, ok := reg.Encoder(key)
		if !ok {
			t.Fatalf("missing encoder for %+v", key)
		}
		raw, p := enc.Encode(snap)
		if p != nil {
			t.Fatalf("encode failed for %+v: %s", key, p.Message)
		}
		dec, ok := reg.Decoder(key)
		if !ok {
			t.Fatalf("missing decoder for %+v", key)
		}
		decoded, p := dec.Decode(raw)
		if p != nil {
			t.Fatalf("decode failed for %+v: %s", key, p.Message)
		}
		if decoded == nil {
			t.Fatalf("decoded payload is nil for %+v", key)
		}
	}
}

// --- PortfolioSummaryV1 round-trip ---

func TestPortfolioSummaryV1_DomainProtoRoundTrip(t *testing.T) {
	original := fixturePortfolioSummary()
	proto := contracts.DomainToProtoPortfolioSummaryV1(original)
	roundTripped := contracts.ProtoToDomainPortfolioSummaryV1(proto)

	if !reflect.DeepEqual(original, roundTripped) {
		t.Fatalf("round-trip mismatch:\n  original:     %+v\n  roundTripped:  %+v", original, roundTripped)
	}
}

func TestPortfolioSummaryV1_NilProtoReturnsZero(t *testing.T) {
	got := contracts.ProtoToDomainPortfolioSummaryV1(nil)
	if got.SummaryID != "" {
		t.Fatal("expected zero value for nil proto")
	}
}

func TestPortfolioSummaryV1_CodecRoundTrip(t *testing.T) {
	reg := codec.NewRegistry()
	if p := contracts.RegisterPortfolioPayloadV1(reg); p != nil {
		t.Fatalf("register failed: %s", p.Message)
	}

	summary := fixturePortfolioSummary()
	for _, format := range []codec.Format{codec.FormatJSON, codec.FormatProto} {
		key := codec.SchemaKey{Type: portfoliodomain.SummaryEventType, Version: 1, Format: format}
		enc, ok := reg.Encoder(key)
		if !ok {
			t.Fatalf("missing encoder for %+v", key)
		}
		raw, p := enc.Encode(summary)
		if p != nil {
			t.Fatalf("encode failed for %+v: %s", key, p.Message)
		}
		dec, ok := reg.Decoder(key)
		if !ok {
			t.Fatalf("missing decoder for %+v", key)
		}
		decoded, p := dec.Decode(raw)
		if p != nil {
			t.Fatalf("decode failed for %+v: %s", key, p.Message)
		}
		if decoded == nil {
			t.Fatalf("decoded payload is nil for %+v", key)
		}
	}
}

// --- Query converters ---

func TestPortfolioStateQueryRequest_RoundTrip(t *testing.T) {
	original := portfoliodomain.PortfolioStateQuery{
		AccountID: "paper",
		Venue:     "binance",
		Symbol:    "BTCUSDT",
		Limit:     10,
	}
	proto := contracts.DomainToProtoPortfolioStateQueryRequest(original)
	roundTripped := contracts.ProtoToDomainPortfolioStateQueryRequest(proto)
	if !reflect.DeepEqual(original, roundTripped) {
		t.Fatalf("round-trip mismatch: %+v vs %+v", original, roundTripped)
	}
}

func TestAccountSnapshotQueryRequest_RoundTrip(t *testing.T) {
	original := portfoliodomain.AccountSnapshotQuery{
		AccountID: "paper",
		FromMs:    1_700_000_000_000,
		ToMs:      1_700_000_060_000,
		Limit:     5,
	}
	proto := contracts.DomainToProtoAccountSnapshotQueryRequest(original)
	roundTripped := contracts.ProtoToDomainAccountSnapshotQueryRequest(proto)
	if !reflect.DeepEqual(original, roundTripped) {
		t.Fatalf("round-trip mismatch: %+v vs %+v", original, roundTripped)
	}
}

func TestPortfolioSummaryQueryRequest_RoundTrip(t *testing.T) {
	original := portfoliodomain.PortfolioSummaryQuery{
		FromMs: 1_700_000_000_000,
		ToMs:   1_700_000_060_000,
		Limit:  3,
	}
	proto := contracts.DomainToProtoPortfolioSummaryQueryRequest(original)
	roundTripped := contracts.ProtoToDomainPortfolioSummaryQueryRequest(proto)
	if !reflect.DeepEqual(original, roundTripped) {
		t.Fatalf("round-trip mismatch: %+v vs %+v", original, roundTripped)
	}
}

func TestQueryConverters_NilReturnsZero(t *testing.T) {
	if q := contracts.ProtoToDomainPortfolioStateQueryRequest(nil); q.AccountID != "" {
		t.Fatal("expected zero")
	}
	if q := contracts.ProtoToDomainAccountSnapshotQueryRequest(nil); q.AccountID != "" {
		t.Fatal("expected zero")
	}
	if q := contracts.ProtoToDomainPortfolioSummaryQueryRequest(nil); q.Limit != 0 {
		t.Fatal("expected zero")
	}
}

// --- Fixtures ---

func fixtureAccountSnapshot() portfoliodomain.AccountSnapshotV1 {
	return portfoliodomain.AccountSnapshotV1{
		SnapshotID:    "snap-1",
		AccountID:     "paper",
		ProjectedAtMs: 1_700_000_001_500,
		Venues: []portfoliodomain.VenuePositionV1{
			{
				Venue: "binance",
				Positions: []portfoliodomain.PositionV1{
					{Venue: "binance", Symbol: "BTCUSDT", Quantity: 1.5, AvgEntryPrice: 100, NotionalUSD: 150, RealizedPnL: 10, UnrealizedPnL: 5, TradeCount: 3, VolumeTradedUSD: 300, LastFillMs: 1_700_000_001_000, Side: "long"},
				},
				Balances: []portfoliodomain.BalanceV1{
					{Asset: "BTC", Total: 1.5, Available: 1.0, Locked: 0.5},
					{Asset: "USDT", Total: 8500, Available: 8000, Locked: 500},
				},
				EquityUSD:        8650,
				RealizedPnlUSD:   10,
				UnrealizedPnlUSD: 5,
				MarginUsedUSD:    15,
			},
		},
		TotalEquityUSD:     8650,
		TotalRealizedUSD:   10,
		TotalUnrealizedUSD: 5,
		TotalMarginUsedUSD: 15,
		TotalLeverage:      0.02,
		FillSummary: portfoliodomain.FillSummaryV1{
			TotalTradeCount:      3,
			TotalVolumeTradedUSD: 300,
			WinCount:             2,
			LossCount:            1,
			LargestWinUSD:        8,
			LargestLossUSD:       -2,
			TurnoverUSD:          300,
		},
	}
}

func fixturePortfolioSummary() portfoliodomain.PortfolioSummaryV1 {
	return portfoliodomain.PortfolioSummaryV1{
		SummaryID:     "sum-1",
		ProjectedAtMs: 1_700_000_001_500,
		Accounts: []portfoliodomain.AccountSummaryV1{
			{AccountID: "paper", VenueCount: 2, PositionCount: 3, EquityUSD: 8650, RealizedPnlUSD: 10, UnrealizedPnlUSD: 5},
			{AccountID: "live", VenueCount: 1, PositionCount: 1, EquityUSD: 50000, RealizedPnlUSD: 200, UnrealizedPnlUSD: -50},
		},
		GlobalEquityUSD:     58650,
		GlobalRealizedUSD:   210,
		GlobalUnrealizedUSD: -45,
		GlobalMarginUsedUSD: 500,
		GlobalLeverage:      0.05,
		TotalPositionCount:  4,
		TotalOpenOrders:     2,
		FillSummary: portfoliodomain.FillSummaryV1{
			TotalTradeCount:      10,
			TotalVolumeTradedUSD: 5000,
			WinCount:             6,
			LossCount:            4,
			LargestWinUSD:        50,
			LargestLossUSD:       -20,
			TurnoverUSD:          5000,
		},
	}
}
