package domain_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
)

func TestPeriodLetter(t *testing.T) {
	tests := []struct {
		idx  int
		want byte
	}{
		{0, 'A'}, {1, 'B'}, {23, 'X'}, {-1, '?'}, {24, '?'},
	}
	for _, tt := range tests {
		got := domain.PeriodLetter(tt.idx)
		if got != tt.want {
			t.Errorf("PeriodLetter(%d): got %c, want %c", tt.idx, got, tt.want)
		}
	}
}

func TestPeriodIndex(t *testing.T) {
	sessionStart := int64(1772870400000)
	thirtyMin := int64(30 * 60 * 1000)

	tests := []struct {
		ts   int64
		want int
	}{
		{sessionStart, 0},                 // period A
		{sessionStart + thirtyMin, 1},     // period B
		{sessionStart + thirtyMin*23, 23}, // period X
		{sessionStart + thirtyMin*25, 23}, // capped at X
		{sessionStart - 1000, 0},          // before session = 0
	}
	for _, tt := range tests {
		got := domain.PeriodIndex(sessionStart, tt.ts)
		if got != tt.want {
			t.Errorf("PeriodIndex(%d): got %d, want %d", tt.ts-sessionStart, got, tt.want)
		}
	}
}

func TestTPOProfile_Valid(t *testing.T) {
	tpo := domain.TPOProfileV1{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Anchor: domain.SessionAnchor{
			Kind: domain.SessionAnchorUTC, Label: "UTC_DAILY",
			Timezone: "UTC", DurationMs: 86400000,
		},
		Periods: []domain.TPOPeriod{
			{Letter: 'A', StartMs: 1000, EndMs: 2000, HighPrice: 102, LowPrice: 99},
		},
		Levels: []domain.TPOLevel{
			{PriceLow: 100, PriceHigh: 101, Letters: []byte{'A'}, Count: 1},
		},
		POCPrice:      100,
		ValueAreaLow:  100,
		ValueAreaHigh: 101,
		RangeHigh:     102,
		RangeLow:      99,
		WindowStartTs: 1000,
		WindowEndTs:   86401000,
	}
	if p := tpo.Validate(); p != nil {
		t.Fatalf("unexpected error: %v", p)
	}
}

func TestTPOProfile_EmptyPeriods(t *testing.T) {
	tpo := domain.TPOProfileV1{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Anchor: domain.SessionAnchor{
			Kind: domain.SessionAnchorUTC, Label: "UTC_DAILY",
			Timezone: "UTC", DurationMs: 86400000,
		},
		Levels:        []domain.TPOLevel{{PriceLow: 100, PriceHigh: 101, Letters: []byte{'A'}, Count: 1}},
		POCPrice:      100,
		ValueAreaLow:  100,
		ValueAreaHigh: 101,
		WindowStartTs: 1000,
		WindowEndTs:   86401000,
	}
	if p := tpo.Validate(); p == nil {
		t.Fatal("expected error for empty periods")
	}
}
