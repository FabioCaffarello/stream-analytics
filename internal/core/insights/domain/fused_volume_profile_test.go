package domain

import (
	"testing"
)

func twoVenueVPVRProfiles() []VolumeProfileSnapshotV1 {
	return []VolumeProfileSnapshotV1{
		{
			Venue: "BINANCE", Instrument: "BTCUSDT", Timeframe: "1h",
			WindowStartTs: 1000, WindowEndTs: 2000,
			Buckets: []VolumeProfileBucketV1{
				{PriceLow: 100, PriceHigh: 101, BuyVolume: 10, SellVolume: 5, TotalVolume: 15, SeqMin: 1, SeqMax: 10},
				{PriceLow: 101, PriceHigh: 102, BuyVolume: 8, SellVolume: 7, TotalVolume: 15, SeqMin: 11, SeqMax: 20},
			},
			POCPrice: 100, ValueAreaLow: 100, ValueAreaHigh: 102,
		},
		{
			Venue: "BYBIT", Instrument: "BTCUSDT", Timeframe: "1h",
			WindowStartTs: 1000, WindowEndTs: 2000,
			Buckets: []VolumeProfileBucketV1{
				{PriceLow: 100, PriceHigh: 101, BuyVolume: 12, SellVolume: 3, TotalVolume: 15, SeqMin: 1, SeqMax: 8},
				{PriceLow: 102, PriceHigh: 103, BuyVolume: 6, SellVolume: 4, TotalVolume: 10, SeqMin: 9, SeqMax: 15},
			},
			POCPrice: 100, ValueAreaLow: 100, ValueAreaHigh: 103,
		},
	}
}

func fuseTwoVenueVPVR(t *testing.T) FusedVolumeProfileSnapshotV1 {
	t.Helper()
	fused, prob := FuseVolumeProfiles("BTCUSDT", "1h", twoVenueVPVRProfiles(), FusionMerge)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	return fused
}

func TestFuseVolumeProfiles_MergeHeader(t *testing.T) {
	fused := fuseTwoVenueVPVR(t)
	if fused.Instrument != "BTCUSDT" {
		t.Fatalf("instrument=%s want=BTCUSDT", fused.Instrument)
	}
	if fused.Mode != FusionMerge {
		t.Fatalf("mode=%s want=merge", fused.Mode)
	}
	if len(fused.Buckets) != 3 {
		t.Fatalf("buckets len=%d want=3", len(fused.Buckets))
	}
}

func TestFuseVolumeProfiles_MergedBucket(t *testing.T) {
	fused := fuseTwoVenueVPVR(t)
	for _, b := range fused.Buckets {
		if b.PriceLow == 100 && b.PriceHigh == 101 {
			if b.BuyVolume != 22 {
				t.Fatalf("buy_volume=%f want=22", b.BuyVolume)
			}
			if b.SellVolume != 8 {
				t.Fatalf("sell_volume=%f want=8", b.SellVolume)
			}
			if b.TotalVolume != 30 {
				t.Fatalf("total_volume=%f want=30", b.TotalVolume)
			}
			if len(b.VenueMix) != 2 {
				t.Fatalf("venue_mix len=%d want=2", len(b.VenueMix))
			}
			return
		}
	}
	t.Fatal("merged bucket 100-101 not found")
}

func TestFuseVolumeProfiles_SourcesAndPOC(t *testing.T) {
	fused := fuseTwoVenueVPVR(t)
	if len(fused.SourceVenues) != 2 {
		t.Fatalf("source_venues len=%d want=2", len(fused.SourceVenues))
	}
	if fused.SourceVenues[0] != "BINANCE" || fused.SourceVenues[1] != "BYBIT" {
		t.Fatalf("source_venues=%v want=[BINANCE,BYBIT]", fused.SourceVenues)
	}
	if fused.POCPrice != 100 {
		t.Fatalf("poc_price=%f want=100", fused.POCPrice)
	}
	if p := fused.Validate(); p != nil {
		t.Fatalf("validation failed: %v", p)
	}
}

func TestFuseVolumeProfiles_BucketsSortedByPrice(t *testing.T) {
	profiles := []VolumeProfileSnapshotV1{
		{
			Venue: "BINANCE", Instrument: "ETHUSDT", Timeframe: "1m",
			WindowStartTs: 1000, WindowEndTs: 2000,
			Buckets: []VolumeProfileBucketV1{
				{PriceLow: 200, PriceHigh: 201, BuyVolume: 5, SellVolume: 5, TotalVolume: 10, SeqMin: 1, SeqMax: 5},
				{PriceLow: 198, PriceHigh: 199, BuyVolume: 3, SellVolume: 3, TotalVolume: 6, SeqMin: 6, SeqMax: 10},
			},
			POCPrice: 200, ValueAreaLow: 198, ValueAreaHigh: 201,
		},
	}
	fused, prob := FuseVolumeProfiles("ETHUSDT", "1m", profiles, FusionSingleVenue)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	for i := 1; i < len(fused.Buckets); i++ {
		if fused.Buckets[i].PriceLow < fused.Buckets[i-1].PriceLow {
			t.Fatalf("buckets not sorted at index %d", i)
		}
	}
}

func TestFuseVolumeProfiles_EmptyProfilesRejected(t *testing.T) {
	_, prob := FuseVolumeProfiles("BTCUSDT", "1h", nil, FusionMerge)
	if prob == nil {
		t.Fatal("expected error for empty profiles")
	}
}

func TestFuseVolumeProfiles_VenueMixWeightsPctSumTo100(t *testing.T) {
	profiles := []VolumeProfileSnapshotV1{
		{
			Venue: "A", Instrument: "X", Timeframe: "1h",
			WindowStartTs: 1000, WindowEndTs: 2000,
			Buckets: []VolumeProfileBucketV1{
				{PriceLow: 10, PriceHigh: 11, BuyVolume: 6, SellVolume: 4, TotalVolume: 10, SeqMin: 1, SeqMax: 5},
			},
			POCPrice: 10, ValueAreaLow: 10, ValueAreaHigh: 11,
		},
		{
			Venue: "B", Instrument: "X", Timeframe: "1h",
			WindowStartTs: 1000, WindowEndTs: 2000,
			Buckets: []VolumeProfileBucketV1{
				{PriceLow: 10, PriceHigh: 11, BuyVolume: 14, SellVolume: 6, TotalVolume: 20, SeqMin: 1, SeqMax: 8},
			},
			POCPrice: 10, ValueAreaLow: 10, ValueAreaHigh: 11,
		},
	}
	fused, prob := FuseVolumeProfiles("X", "1h", profiles, FusionMerge)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	b := fused.Buckets[0]
	var sum float64
	for _, v := range b.VenueMix {
		sum += v.WeightPct
	}
	if sum < 99.9 || sum > 100.1 {
		t.Fatalf("venue_mix weight_pct sum=%f want~100", sum)
	}
}
