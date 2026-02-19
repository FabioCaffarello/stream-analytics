package domain_test

import (
	"testing"

	"github.com/market-raccoon/internal/core/insights/domain"
)

func TestVPVRBucketDeterministic(t *testing.T) {
	low1, high1, p1 := domain.AssignVPVRBucket(101.73, 0.5)
	low2, high2, p2 := domain.AssignVPVRBucket(101.73, 0.5)
	if p1 != nil || p2 != nil {
		t.Fatalf("unexpected bucket assignment problem: %v / %v", p1, p2)
	}
	if low1 != low2 || high1 != high2 {
		t.Fatalf("bucket assignment must be deterministic: (%v,%v) != (%v,%v)", low1, high1, low2, high2)
	}
}

func TestVPVRCardinalityCap(t *testing.T) {
	s := domain.VolumeProfileSnapshotV1{
		Venue:         "BINANCE",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1710000000000,
		WindowEndTs:   1710000060000,
		POCPrice:      100,
		ValueAreaLow:  99,
		ValueAreaHigh: 102,
	}
	for i := 0; i < domain.VPVRCapBucketsPerWindow+1; i++ {
		price := float64(100 + i)
		s.Buckets = append(s.Buckets, domain.VolumeProfileBucketV1{
			PriceLow:    price,
			PriceHigh:   price + 1,
			BuyVolume:   1,
			SellVolume:  1,
			TotalVolume: 2,
			SeqMin:      int64(i + 1),
			SeqMax:      int64(i + 1),
		})
	}
	if p := s.Validate(); p == nil {
		t.Fatal("expected cardinality cap validation error")
	}
}

func TestVPVRPointOfControlConsistency(t *testing.T) {
	s := domain.VolumeProfileSnapshotV1{
		Venue:         "BINANCE",
		Instrument:    "BTCUSDT",
		Timeframe:     "1m",
		WindowStartTs: 1710000000000,
		WindowEndTs:   1710000060000,
		Buckets: []domain.VolumeProfileBucketV1{
			{PriceLow: 100, PriceHigh: 101, BuyVolume: 1, SellVolume: 1, TotalVolume: 2, SeqMin: 1, SeqMax: 1},
			{PriceLow: 101, PriceHigh: 102, BuyVolume: 3, SellVolume: 1, TotalVolume: 4, SeqMin: 2, SeqMax: 2},
		},
		POCPrice:      100,
		ValueAreaLow:  100,
		ValueAreaHigh: 102,
	}
	if p := s.Validate(); p == nil {
		t.Fatal("expected poc validation error")
	}
}
