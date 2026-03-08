package domain

import (
	"encoding/json"
	"sort"
	"testing"
)

// TestWireFormatGolden_CandleV1 asserts the JSON key set for CandleV1 is frozen.
// Any field addition, removal, or rename will cause this test to fail,
// preventing accidental wire format changes.
func TestWireFormatGolden_CandleV1(t *testing.T) {
	frozen := []string{
		"Venue", "Instrument", "Timeframe",
		"WindowStartTs", "WindowEndTs",
		"Open", "High", "Low", "ClosePrice",
		"Volume", "BuyVolume", "SellVolume",
		"TradeCount", "SeqFirst", "SeqLast", "IsClosed",
	}
	assertJSONKeys(t, "CandleV1", CandleV1{}, frozen)
}

// TestWireFormatGolden_StatsWindowV1 asserts the JSON key set for StatsWindowV1 is frozen.
func TestWireFormatGolden_StatsWindowV1(t *testing.T) {
	frozen := []string{
		"Venue", "Instrument", "Timeframe",
		"WindowStartTs", "WindowEndTs", "WindowMs", "TsIngestMs",
		"QualityFlags",
		"LiqBuyVolume", "LiqSellVolume", "LiqTotalVolume", "LiqCount",
		"MarkPriceOpen", "MarkPriceHigh", "MarkPriceLow", "MarkPriceClose",
		"FundingRateAvg", "FundingRateLast",
		"SeqFirst", "SeqLast", "IsClosed",
	}
	assertJSONKeys(t, "StatsWindowV1", StatsWindowV1{}, frozen)
}

// TestWireFormatGolden_TapeWindowV1 asserts the JSON key set for TapeWindowV1 is frozen.
func TestWireFormatGolden_TapeWindowV1(t *testing.T) {
	frozen := []string{
		"Venue", "Instrument", "Timeframe",
		"WindowStartTs", "WindowEndTs",
		"TradeCount", "BuyCount", "SellCount",
		"BuyVolume", "SellVolume", "TotalVolume",
		"BuyNotional", "SellNotional",
		"VwapPrice", "MaxPrice", "MinPrice", "LastPrice",
		"MaxTradeSize", "LastSeq",
		"RateTradesPerSec", "VolumeImbalance",
		"IsClosed",
	}
	assertJSONKeys(t, "TapeWindowV1", TapeWindowV1{}, frozen)
}

// TestWireFormatGolden_DeltaVolumeWindowV1 asserts the JSON key set is frozen.
func TestWireFormatGolden_DeltaVolumeWindowV1(t *testing.T) {
	frozen := []string{
		"Venue", "Instrument", "Timeframe",
		"WindowStartTs", "WindowEndTs",
		"BuyVolume", "SellVolume", "DeltaVolume",
		"Seq", "TsIngestMs",
	}
	assertJSONKeys(t, "DeltaVolumeWindowV1", DeltaVolumeWindowV1{}, frozen)
}

// TestWireFormatGolden_CVDWindowV1 asserts the JSON key set is frozen.
func TestWireFormatGolden_CVDWindowV1(t *testing.T) {
	frozen := []string{
		"Venue", "Instrument", "Timeframe",
		"WindowStartTs", "WindowEndTs",
		"DeltaVolume", "CVD",
		"Seq", "TsIngestMs",
	}
	assertJSONKeys(t, "CVDWindowV1", CVDWindowV1{}, frozen)
}

// TestWireFormatGolden_BarStatsWindowV1 asserts the JSON key set is frozen.
func TestWireFormatGolden_BarStatsWindowV1(t *testing.T) {
	frozen := []string{
		"Venue", "Instrument", "Timeframe",
		"WindowStartTs", "WindowEndTs",
		"TradeCount", "BuyCount", "SellCount",
		"TotalVolume", "BuyVolume", "SellVolume",
		"VwapPrice", "LastPrice", "MaxPrice", "MinPrice",
		"Imbalance", "IsBurst",
		"Seq", "TsIngestMs",
	}
	assertJSONKeys(t, "BarStatsWindowV1", BarStatsWindowV1{}, frozen)
}

// TestWireFormatGolden_OpenInterestWindowV1 asserts the JSON key set is frozen.
func TestWireFormatGolden_OpenInterestWindowV1(t *testing.T) {
	frozen := []string{
		"CadenceHintMs", "Confidence",
		"Delta", "DeltaPct",
		"Instrument",
		"OpenInterest",
		"Seq",
		"Timeframe", "TsIngestMs",
		"Venue",
		"WindowEndTs", "WindowStartTs",
	}
	assertJSONKeys(t, "OpenInterestWindowV1", OpenInterestWindowV1{}, frozen)
}

// TestWireFormatGolden_SnapshotProduced asserts the JSON key set is frozen.
func TestWireFormatGolden_SnapshotProduced(t *testing.T) {
	frozen := []string{
		"BookID", "Seq", "Bids", "Asks",
		"BestBidPrice", "BestAskPrice", "SpreadBPS",
		"Checksum", "TsIngestMs",
		"BidCount", "AskCount", "DepthCap", "Version",
	}
	assertJSONKeys(t, "SnapshotProduced", SnapshotProduced{}, frozen)
}

// assertJSONKeys marshals v to JSON and verifies the top-level key set matches frozen exactly.
func assertJSONKeys(t *testing.T, typeName string, v any, frozen []string) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("%s: json.Marshal failed: %v", typeName, err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s: json.Unmarshal to map failed: %v", typeName, err)
	}
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	want := make([]string, len(frozen))
	copy(want, frozen)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("%s: key count mismatch: got %d, want %d\n  got:  %v\n  want: %v", typeName, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: key mismatch at index %d: got %q, want %q\n  got:  %v\n  want: %v", typeName, i, got[i], want[i], got, want)
		}
	}
}
