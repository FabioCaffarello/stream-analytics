package domain_test

import (
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/insights/domain"
)

func TestSessionVolumeProfile_Valid(t *testing.T) {
	svp := domain.SessionVolumeProfileV1{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Anchor: domain.SessionAnchor{
			Kind: domain.SessionAnchorUTC, Label: "UTC_DAILY",
			Timezone: "UTC", DurationMs: 86400000,
		},
		Buckets: []domain.VolumeProfileBucketV1{
			{PriceLow: 100, PriceHigh: 101, BuyVolume: 5, SellVolume: 3, TotalVolume: 8, SeqMin: 1, SeqMax: 10},
			{PriceLow: 101, PriceHigh: 102, BuyVolume: 2, SellVolume: 1, TotalVolume: 3, SeqMin: 11, SeqMax: 20},
		},
		POCPrice:      100,
		ValueAreaLow:  100,
		ValueAreaHigh: 102,
		TotalVolume:   11,
		BuyVolume:     7,
		SellVolume:    4,
		TradeCount:    20,
		WindowStartTs: 1772870400000,
		WindowEndTs:   1772956800000,
		SeqFirst:      1,
		SeqLast:       20,
	}
	if p := svp.Validate(); p != nil {
		t.Fatalf("unexpected validation error: %v", p)
	}
}

func TestSessionVolumeProfile_WrongPOC(t *testing.T) {
	svp := domain.SessionVolumeProfileV1{
		Venue:      "binance",
		Instrument: "BTCUSDT",
		Anchor: domain.SessionAnchor{
			Kind: domain.SessionAnchorUTC, Label: "UTC_DAILY",
			Timezone: "UTC", DurationMs: 86400000,
		},
		Buckets: []domain.VolumeProfileBucketV1{
			{PriceLow: 100, PriceHigh: 101, BuyVolume: 1, SellVolume: 1, TotalVolume: 2, SeqMin: 1, SeqMax: 1},
			{PriceLow: 101, PriceHigh: 102, BuyVolume: 5, SellVolume: 5, TotalVolume: 10, SeqMin: 2, SeqMax: 2},
		},
		POCPrice:      100, // wrong — should be 101
		ValueAreaLow:  100,
		ValueAreaHigh: 102,
		TotalVolume:   12,
		BuyVolume:     6,
		SellVolume:    6,
		TradeCount:    2,
		WindowStartTs: 1772870400000,
		WindowEndTs:   1772956800000,
		SeqFirst:      1,
		SeqLast:       2,
	}
	if p := svp.Validate(); p == nil {
		t.Fatal("expected validation error for wrong POC")
	}
}

func TestComputePOC(t *testing.T) {
	buckets := []domain.VolumeProfileBucketV1{
		{PriceLow: 100, PriceHigh: 101, TotalVolume: 5},
		{PriceLow: 101, PriceHigh: 102, TotalVolume: 20},
		{PriceLow: 102, PriceHigh: 103, TotalVolume: 8},
	}
	poc, idx := domain.ComputePOC(buckets)
	if poc != 101 || idx != 1 {
		t.Fatalf("poc: got (%f, %d), want (101, 1)", poc, idx)
	}
}

func TestComputeValueArea(t *testing.T) {
	buckets := []domain.VolumeProfileBucketV1{
		{PriceLow: 98, PriceHigh: 99, TotalVolume: 2},
		{PriceLow: 99, PriceHigh: 100, TotalVolume: 5},
		{PriceLow: 100, PriceHigh: 101, TotalVolume: 30},
		{PriceLow: 101, PriceHigh: 102, TotalVolume: 8},
		{PriceLow: 102, PriceHigh: 103, TotalVolume: 1},
	}
	_, pocIdx := domain.ComputePOC(buckets)
	vah, val := domain.ComputeValueArea(buckets, pocIdx, 0.70)
	if val > buckets[pocIdx].PriceLow {
		t.Errorf("VAL (%f) should be <= POC (%f)", val, buckets[pocIdx].PriceLow)
	}
	if vah < buckets[pocIdx].PriceHigh {
		t.Errorf("VAH (%f) should be >= POC high (%f)", vah, buckets[pocIdx].PriceHigh)
	}
}

func TestComputePOC_Empty(t *testing.T) {
	poc, idx := domain.ComputePOC(nil)
	if poc != 0 || idx != -1 {
		t.Fatalf("expected (0, -1), got (%f, %d)", poc, idx)
	}
}
