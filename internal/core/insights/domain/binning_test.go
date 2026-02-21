package domain_test

import (
	"math"
	"testing"

	"github.com/market-raccoon/internal/core/insights/domain"
)

// goldenBinCase captures one price/tickSize pair with expected bin sizes
// computed from the MarketMonkey reference algorithm.
type goldenBinCase struct {
	name           string
	price          float64
	tickSize       float64
	wantVolumeBin  float64
	wantHeatmapBin float64
}

// goldenBinCases contains 20 price/tickSize pairs covering BTC, ETH,
// altcoins, micro-cap tokens, and edge cases.  Expected values are computed
// by running the exact MM CalculateBinSize algorithm.
var goldenBinCases = []goldenBinCase{
	// --- BTC range ---
	{name: "BTC_50000_001", price: 50000, tickSize: 0.01,
		wantVolumeBin: 2.5, wantHeatmapBin: 10},
	{name: "BTC_45000_01", price: 45000, tickSize: 0.1,
		wantVolumeBin: 2, wantHeatmapBin: 10},
	{name: "BTC_100000_001", price: 100000, tickSize: 0.01,
		wantVolumeBin: 5, wantHeatmapBin: 25},
	{name: "BTC_30000_001", price: 30000, tickSize: 0.01,
		wantVolumeBin: 1, wantHeatmapBin: 5},

	// --- ETH range ---
	{name: "ETH_3000_001", price: 3000, tickSize: 0.01,
		wantVolumeBin: 0.1, wantHeatmapBin: 0.5},
	{name: "ETH_4000_01", price: 4000, tickSize: 0.01,
		wantVolumeBin: 0.2, wantHeatmapBin: 1},
	{name: "ETH_2000_001", price: 2000, tickSize: 0.01,
		wantVolumeBin: 0.1, wantHeatmapBin: 0.5},

	// --- Mid-cap alts ---
	{name: "SOL_100_001", price: 100, tickSize: 0.01,
		wantVolumeBin: 0.01, wantHeatmapBin: 0.02},
	{name: "SOL_200_001", price: 200, tickSize: 0.01,
		wantVolumeBin: 0.01, wantHeatmapBin: 0.05},
	{name: "AVAX_50_001", price: 50, tickSize: 0.01,
		wantVolumeBin: 0.01, wantHeatmapBin: 0.01},

	// --- Low-priced tokens ---
	// DOGE: n_vol=5e-9, no tick-div step → tickSize floor.
	// n_heat=2.5e-6, step 0.20*1e-5=2e-6 is tick-div (2e-6/1e-5=0.2 → frac=0.2≠0), try 0.10*1e-5=1e-5/1e-5=1.0→frac=0→match? No, 2.5e-6 < 1e-5. Actually factor=1e-4.
	// Recomputed: n_heat=0.1*0.025/100=2.5e-5. factor=1e-4. 0.20*1e-4=2e-5 >=2.5e-5? No. Step 0.25*1e-4=2.5e-5 >=2.5e-5? Yes. 2.5e-5/1e-5=2.5→frac=0.5→skip. 0.20*1e-4=2e-5: 2.5e-5>=2e-5? Yes. 2e-5/1e-5=2→frac=0→match!
	{name: "DOGE_01_00001", price: 0.1, tickSize: 0.00001,
		wantVolumeBin: 0.00001, wantHeatmapBin: 0.00002},
	// SHIB: both bins fall to tickSize floor.
	{name: "SHIB_000001_00000001", price: 0.00001, tickSize: 0.00000001,
		wantVolumeBin: 0.00000001, wantHeatmapBin: 0.00000001},
	// PEPE: n_vol=5e-8, no tick-div step → tickSize floor.
	// n_heat=2.5e-7, factor=1e-6. 0.25*1e-6=2.5e-7→frac(2.5e-7/1e-7)=frac(2.5)=0.5→skip. 0.20*1e-6=2e-7→frac(2e-7/1e-7)=frac(2)=0→match!
	{name: "PEPE_0001_0000001", price: 0.001, tickSize: 0.0000001,
		wantVolumeBin: 0.0000001, wantHeatmapBin: 0.0000002},

	// --- High-priced assets ---
	{name: "STOCK_1000_01", price: 1000, tickSize: 0.01,
		wantVolumeBin: 0.05, wantHeatmapBin: 0.25},
	{name: "INDEX_5000_01", price: 5000, tickSize: 0.01,
		wantVolumeBin: 0.25, wantHeatmapBin: 1},

	// --- Edge: tickSize equals or exceeds computed bin ---
	{name: "TICK_FLOOR_10_5", price: 10, tickSize: 5,
		wantVolumeBin: 5, wantHeatmapBin: 5},
	{name: "TICK_FLOOR_1_1", price: 1, tickSize: 1,
		wantVolumeBin: 1, wantHeatmapBin: 1},

	// --- Very small and very large ---
	{name: "MICRO_0000001_000000001", price: 0.0000001, tickSize: 0.000000001,
		wantVolumeBin: 0.000000001, wantHeatmapBin: 0.000000001},
	{name: "MEGA_500000_01", price: 500000, tickSize: 0.01,
		wantVolumeBin: 25, wantHeatmapBin: 100},
	{name: "CENT_50_0001", price: 50, tickSize: 0.001,
		wantVolumeBin: 0.002, wantHeatmapBin: 0.01},
}

func TestCalculateVolumeBinSize_Golden20(t *testing.T) {
	for _, tc := range goldenBinCases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.CalculateVolumeBinSize(tc.price, tc.tickSize)
			if !binAlmostEqual(got, tc.wantVolumeBin) {
				t.Fatalf("CalculateVolumeBinSize(%v, %v) = %v, want %v",
					tc.price, tc.tickSize, got, tc.wantVolumeBin)
			}
		})
	}
}

func TestCalculateHeatmapBinSize_Golden20(t *testing.T) {
	for _, tc := range goldenBinCases {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.CalculateHeatmapBinSize(tc.price, tc.tickSize)
			if !binAlmostEqual(got, tc.wantHeatmapBin) {
				t.Fatalf("CalculateHeatmapBinSize(%v, %v) = %v, want %v",
					tc.price, tc.tickSize, got, tc.wantHeatmapBin)
			}
		})
	}
}

func TestCalculateBinSize_AlwaysGETickSize(t *testing.T) {
	for _, tc := range goldenBinCases {
		volBin := domain.CalculateVolumeBinSize(tc.price, tc.tickSize)
		if volBin < tc.tickSize-1e-12 {
			t.Fatalf("%s: volume bin %v < tickSize %v", tc.name, volBin, tc.tickSize)
		}
		heatBin := domain.CalculateHeatmapBinSize(tc.price, tc.tickSize)
		if heatBin < tc.tickSize-1e-12 {
			t.Fatalf("%s: heatmap bin %v < tickSize %v", tc.name, heatBin, tc.tickSize)
		}
	}
}

func TestCalculateBinSize_AlwaysTickDivisible(t *testing.T) {
	for _, tc := range goldenBinCases {
		volBin := domain.CalculateVolumeBinSize(tc.price, tc.tickSize)
		assertTickDivisible(t, tc.name+"_vol", volBin, tc.tickSize)
		heatBin := domain.CalculateHeatmapBinSize(tc.price, tc.tickSize)
		assertTickDivisible(t, tc.name+"_heat", heatBin, tc.tickSize)
	}
}

func TestCalculateBinSize_MinimumBound(t *testing.T) {
	// When tickSize >= 1e-8, the bin size must be >= 1e-8.
	got := domain.CalculateBinSize(0.01, 0.00000001, 0.005)
	if got < 0.00000001 {
		t.Fatalf("bin size %v below minimum 1e-8", got)
	}
	// When tickSize < 1e-8, tickSize floor takes priority (matching MM).
	got = domain.CalculateBinSize(0.0000001, 0.000000001, 0.005)
	if got < 0.000000001 {
		t.Fatalf("bin size %v below tickSize 1e-9", got)
	}
}

func BenchmarkCalculateBinSize(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		domain.CalculateBinSize(50000, 0.01, 0.025)
	}
}

func BenchmarkCalculateVolumeBinSize(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		domain.CalculateVolumeBinSize(50000, 0.01)
	}
}

func BenchmarkCalculateHeatmapBinSize(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		domain.CalculateHeatmapBinSize(50000, 0.01)
	}
}

func binAlmostEqual(a, b float64) bool {
	if a == b {
		return true
	}
	return math.Abs(a-b) < 1e-10
}

func assertTickDivisible(t *testing.T, name string, binSize, tickSize float64) {
	t.Helper()
	ratio := binSize / tickSize
	rounded := math.Round(ratio)
	if math.Abs(ratio-rounded) > 1e-6 {
		t.Fatalf("%s: bin %v not divisible by tick %v (ratio=%v)", name, binSize, tickSize, ratio)
	}
}
