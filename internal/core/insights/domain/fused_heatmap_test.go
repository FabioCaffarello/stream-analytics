package domain

import (
	"testing"
)

func twoVenueHeatmapInputs() []HeatmapArtifactV1 {
	return []HeatmapArtifactV1{
		{
			Venue: "BINANCE", Instrument: "BTCUSDT", Timeframe: "1h",
			WindowStartTs: 1000, WindowEndTs: 2000,
			Cells: []HeatmapCellV1{
				{PriceBucketLow: 100, PriceBucketHigh: 101, SizeBucket: "LARGE", BidLiquidity: 10, AskLiquidity: 5, TradeVolume: 3, SeqMin: 1, SeqMax: 5, Samples: 10},
				{PriceBucketLow: 101, PriceBucketHigh: 102, SizeBucket: "SMALL", BidLiquidity: 4, AskLiquidity: 6, TradeVolume: 2, SeqMin: 6, SeqMax: 10, Samples: 8},
			},
		},
		{
			Venue: "BYBIT", Instrument: "BTCUSDT", Timeframe: "1h",
			WindowStartTs: 1000, WindowEndTs: 2000,
			Cells: []HeatmapCellV1{
				{PriceBucketLow: 100, PriceBucketHigh: 101, SizeBucket: "LARGE", BidLiquidity: 8, AskLiquidity: 4, TradeVolume: 2, SeqMin: 1, SeqMax: 4, Samples: 7},
				{PriceBucketLow: 102, PriceBucketHigh: 103, SizeBucket: "MICRO", BidLiquidity: 1, AskLiquidity: 1, TradeVolume: 1, SeqMin: 5, SeqMax: 7, Samples: 3},
			},
		},
	}
}

func fuseTwoVenueHeatmap(t *testing.T) FusedHeatmapArtifactV1 {
	t.Helper()
	fused, prob := FuseHeatmaps("BTCUSDT", "1h", twoVenueHeatmapInputs(), FusionMerge)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	return fused
}

func TestFuseHeatmaps_MergeHeader(t *testing.T) {
	fused := fuseTwoVenueHeatmap(t)
	if fused.Instrument != "BTCUSDT" {
		t.Fatalf("instrument=%s want=BTCUSDT", fused.Instrument)
	}
	if fused.Mode != FusionMerge {
		t.Fatalf("mode=%s want=merge", fused.Mode)
	}
	if len(fused.Cells) != 3 {
		t.Fatalf("cells len=%d want=3", len(fused.Cells))
	}
}

func TestFuseHeatmaps_MergedCell(t *testing.T) {
	fused := fuseTwoVenueHeatmap(t)
	for _, c := range fused.Cells {
		if c.PriceBucketLow == 100 && c.PriceBucketHigh == 101 && c.SizeBucket == "LARGE" {
			if c.BidLiquidity != 18 {
				t.Fatalf("bid_liq=%f want=18", c.BidLiquidity)
			}
			if c.AskLiquidity != 9 {
				t.Fatalf("ask_liq=%f want=9", c.AskLiquidity)
			}
			if c.TradeVolume != 5 {
				t.Fatalf("trade_vol=%f want=5", c.TradeVolume)
			}
			if c.Samples != 17 {
				t.Fatalf("samples=%d want=17", c.Samples)
			}
			if len(c.VenueMix) != 2 {
				t.Fatalf("venue_mix len=%d want=2", len(c.VenueMix))
			}
			return
		}
	}
	t.Fatal("merged cell 100-101/LARGE not found")
}

func TestFuseHeatmaps_MergeSources(t *testing.T) {
	fused := fuseTwoVenueHeatmap(t)
	if len(fused.SourceVenues) != 2 {
		t.Fatalf("source_venues len=%d want=2", len(fused.SourceVenues))
	}
	if fused.SourceVenues[0] != "BINANCE" || fused.SourceVenues[1] != "BYBIT" {
		t.Fatalf("source_venues=%v want=[BINANCE,BYBIT]", fused.SourceVenues)
	}
	if p := fused.Validate(); p != nil {
		t.Fatalf("validation failed: %v", p)
	}
}

func TestFuseHeatmaps_CellsSortedByPriceThenSize(t *testing.T) {
	heatmaps := []HeatmapArtifactV1{
		{
			Venue: "A", Instrument: "X", Timeframe: "1h",
			WindowStartTs: 1000, WindowEndTs: 2000,
			Cells: []HeatmapCellV1{
				{PriceBucketLow: 200, PriceBucketHigh: 201, SizeBucket: "SMALL", BidLiquidity: 1, AskLiquidity: 1, TradeVolume: 1, SeqMin: 1, SeqMax: 1, Samples: 1},
				{PriceBucketLow: 200, PriceBucketHigh: 201, SizeBucket: "LARGE", BidLiquidity: 2, AskLiquidity: 2, TradeVolume: 2, SeqMin: 2, SeqMax: 2, Samples: 1},
				{PriceBucketLow: 198, PriceBucketHigh: 199, SizeBucket: "MICRO", BidLiquidity: 3, AskLiquidity: 3, TradeVolume: 3, SeqMin: 3, SeqMax: 3, Samples: 1},
			},
		},
	}
	fused, prob := FuseHeatmaps("X", "1h", heatmaps, FusionSingleVenue)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	// First cell should be 198-199 (lowest price)
	if fused.Cells[0].PriceBucketLow != 198 {
		t.Fatalf("first cell price_low=%f want=198", fused.Cells[0].PriceBucketLow)
	}
	// At same price, LARGE < SMALL alphabetically
	if fused.Cells[1].SizeBucket != "LARGE" {
		t.Fatalf("second cell size=%s want=LARGE", fused.Cells[1].SizeBucket)
	}
	if fused.Cells[2].SizeBucket != "SMALL" {
		t.Fatalf("third cell size=%s want=SMALL", fused.Cells[2].SizeBucket)
	}
}

func TestFuseHeatmaps_EmptyHeatmapsRejected(t *testing.T) {
	_, prob := FuseHeatmaps("X", "1h", nil, FusionMerge)
	if prob == nil {
		t.Fatal("expected error for empty heatmaps")
	}
}

func TestFuseHeatmaps_CellCapEnforced(t *testing.T) {
	cells := make([]HeatmapCellV1, FusedHeatmapMaxCells+10)
	for i := range cells {
		cells[i] = HeatmapCellV1{
			PriceBucketLow: float64(i), PriceBucketHigh: float64(i + 1),
			SizeBucket: "LARGE", BidLiquidity: 1, AskLiquidity: 1, TradeVolume: 1,
			SeqMin: 1, SeqMax: 1, Samples: 1,
		}
	}
	heatmaps := []HeatmapArtifactV1{
		{Venue: "A", Instrument: "X", Timeframe: "1h", WindowStartTs: 1000, WindowEndTs: 2000, Cells: cells},
	}
	fused, prob := FuseHeatmaps("X", "1h", heatmaps, FusionMerge)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	if len(fused.Cells) > FusedHeatmapMaxCells {
		t.Fatalf("cells=%d want<=%d", len(fused.Cells), FusedHeatmapMaxCells)
	}
}

func TestFuseHeatmaps_WindowBoundsSpanAllInputs(t *testing.T) {
	heatmaps := []HeatmapArtifactV1{
		{
			Venue: "A", Instrument: "X", Timeframe: "1h",
			WindowStartTs: 2000, WindowEndTs: 3000,
			Cells: []HeatmapCellV1{
				{PriceBucketLow: 10, PriceBucketHigh: 11, SizeBucket: "S", BidLiquidity: 1, AskLiquidity: 1, TradeVolume: 1, SeqMin: 1, SeqMax: 1, Samples: 1},
			},
		},
		{
			Venue: "B", Instrument: "X", Timeframe: "1h",
			WindowStartTs: 1000, WindowEndTs: 4000,
			Cells: []HeatmapCellV1{
				{PriceBucketLow: 10, PriceBucketHigh: 11, SizeBucket: "S", BidLiquidity: 2, AskLiquidity: 2, TradeVolume: 2, SeqMin: 2, SeqMax: 2, Samples: 2},
			},
		},
	}
	fused, prob := FuseHeatmaps("X", "1h", heatmaps, FusionMerge)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	if fused.WindowStartTs != 1000 {
		t.Fatalf("window_start=%d want=1000", fused.WindowStartTs)
	}
	if fused.WindowEndTs != 4000 {
		t.Fatalf("window_end=%d want=4000", fused.WindowEndTs)
	}
}
