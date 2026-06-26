package domain

import (
	"testing"
)

func twoVenueMergeInputs() []FusionDepthInput {
	return []FusionDepthInput{
		{
			Venue:      "BINANCE",
			TsIngestMs: 99_000,
			Bids:       []Level{{Price: 100.0, Quantity: 1.0}, {Price: 99.5, Quantity: 2.0}},
			Asks:       []Level{{Price: 101.0, Quantity: 1.5}, {Price: 101.5, Quantity: 2.5}},
		},
		{
			Venue:      "BYBIT",
			TsIngestMs: 99_500,
			Bids:       []Level{{Price: 100.0, Quantity: 0.8}, {Price: 99.0, Quantity: 1.2}},
			Asks:       []Level{{Price: 101.0, Quantity: 1.0}, {Price: 102.0, Quantity: 3.0}},
		},
	}
}

func fuseTwoVenueMerge(t *testing.T) FusedDepthSnapshotV1 {
	t.Helper()
	snap, prob := FuseDepth("BTCUSDT", 100_000, twoVenueMergeInputs(), FusionMerge, 30_000)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	return snap
}

func TestFuseDepth_MergeMode_Header(t *testing.T) {
	snap := fuseTwoVenueMerge(t)
	if snap.Instrument != "BTCUSDT" {
		t.Fatalf("instrument=%s want=BTCUSDT", snap.Instrument)
	}
	if snap.Mode != FusionMerge {
		t.Fatalf("mode=%s want=merge", snap.Mode)
	}
	if len(snap.SourceVenues) != 2 {
		t.Fatalf("source_venues len=%d want=2", len(snap.SourceVenues))
	}
}

func TestFuseDepth_MergeMode_BidsMerged(t *testing.T) {
	snap := fuseTwoVenueMerge(t)
	if len(snap.Bids) < 2 {
		t.Fatalf("bids len=%d want>=2", len(snap.Bids))
	}
	topBid := snap.Bids[0]
	if topBid.PriceFP != fusionPriceToFP(100.0) {
		t.Fatalf("top bid price_fp=%d want=%d", topBid.PriceFP, fusionPriceToFP(100.0))
	}
	wantSizeFP := fusionQtyToFP(1.0) + fusionQtyToFP(0.8)
	if topBid.SizeFP != wantSizeFP {
		t.Fatalf("top bid size_fp=%d want=%d", topBid.SizeFP, wantSizeFP)
	}
	if len(topBid.Venues) != 2 {
		t.Fatalf("top bid venues=%d want=2", len(topBid.Venues))
	}
}

func TestFuseDepth_MergeMode_AsksMerged(t *testing.T) {
	snap := fuseTwoVenueMerge(t)
	if len(snap.Asks) < 2 {
		t.Fatalf("asks len=%d want>=2", len(snap.Asks))
	}
	if snap.Asks[0].PriceFP != fusionPriceToFP(101.0) {
		t.Fatalf("top ask price_fp=%d want=%d", snap.Asks[0].PriceFP, fusionPriceToFP(101.0))
	}
}

func TestFuseDepth_MergeMode_Meta(t *testing.T) {
	snap := fuseTwoVenueMerge(t)
	if snap.GlobalSpreadBPS <= 0 {
		t.Fatalf("global_spread_bps=%f want>0", snap.GlobalSpreadBPS)
	}
	if snap.Meta.Reason != "cross_venue_merge" {
		t.Fatalf("reason=%s want=cross_venue_merge", snap.Meta.Reason)
	}
	if snap.Meta.Confidence <= 0 || snap.Meta.Confidence > 1 {
		t.Fatalf("confidence=%f want in (0,1]", snap.Meta.Confidence)
	}
	if p := snap.Validate(); p != nil {
		t.Fatalf("validation failed: %v", p)
	}
}

func TestFuseDepth_SingleMode_Passthrough(t *testing.T) {
	inputs := []FusionDepthInput{
		{
			Venue:      "BINANCE",
			TsIngestMs: 99_000,
			Bids:       []Level{{Price: 100.0, Quantity: 1.0}},
			Asks:       []Level{{Price: 101.0, Quantity: 1.5}},
		},
	}
	snap, prob := FuseDepth("BTCUSDT", 100_000, inputs, FusionSingleVenue, 30_000)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	if snap.Mode != FusionSingleVenue {
		t.Fatalf("mode=%s want=single", snap.Mode)
	}
	if snap.Meta.Reason != "single_venue" {
		t.Fatalf("reason=%s want=single_venue", snap.Meta.Reason)
	}
	if len(snap.Bids) != 1 || len(snap.Asks) != 1 {
		t.Fatalf("bids=%d asks=%d want=1,1", len(snap.Bids), len(snap.Asks))
	}
}

func TestFuseDepth_StaleVenueExcluded(t *testing.T) {
	inputs := []FusionDepthInput{
		{
			Venue:      "BINANCE",
			TsIngestMs: 50_000, // stale
			Bids:       []Level{{Price: 100.0, Quantity: 1.0}},
			Asks:       []Level{{Price: 101.0, Quantity: 1.5}},
		},
		{
			Venue:      "BYBIT",
			TsIngestMs: 99_000,
			Bids:       []Level{{Price: 100.5, Quantity: 0.8}},
			Asks:       []Level{{Price: 101.2, Quantity: 1.0}},
		},
	}
	snap, prob := FuseDepth("BTCUSDT", 100_000, inputs, FusionMerge, 30_000)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	if len(snap.SourceVenues) != 1 {
		t.Fatalf("source_venues=%d want=1", len(snap.SourceVenues))
	}
	if snap.SourceVenues[0] != "BYBIT" {
		t.Fatalf("source_venues[0]=%s want=BYBIT", snap.SourceVenues[0])
	}
	if snap.Meta.Staleness.StaleCount != 1 {
		t.Fatalf("stale_count=%d want=1", snap.Meta.Staleness.StaleCount)
	}
}

func TestFuseDepth_EmptyInputs(t *testing.T) {
	snap, prob := FuseDepth("BTCUSDT", 100_000, nil, FusionMerge, 30_000)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	if len(snap.Bids) != 0 || len(snap.Asks) != 0 {
		t.Fatalf("expected empty bids/asks")
	}
}

func TestFuseDepth_DepthCapped(t *testing.T) {
	levels := make([]Level, FusedDepthMaxLevels+10)
	for i := range levels {
		levels[i] = Level{Price: Price(100 + float64(i)), Quantity: 1.0}
	}
	inputs := []FusionDepthInput{
		{Venue: "BINANCE", TsIngestMs: 99_000, Bids: levels, Asks: levels},
	}
	snap, prob := FuseDepth("BTCUSDT", 100_000, inputs, FusionMerge, 30_000)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	if len(snap.Bids) > FusedDepthMaxLevels {
		t.Fatalf("bids=%d want<=%d", len(snap.Bids), FusedDepthMaxLevels)
	}
	if len(snap.Asks) > FusedDepthMaxLevels {
		t.Fatalf("asks=%d want<=%d", len(snap.Asks), FusedDepthMaxLevels)
	}
	found := false
	for _, tag := range snap.Meta.FeatureTags {
		if tag == "depth_capped" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing depth_capped feature tag")
	}
}

func TestFuseDepth_BidsSortedDescAsksSortedAsc(t *testing.T) {
	inputs := []FusionDepthInput{
		{
			Venue:      "BINANCE",
			TsIngestMs: 99_000,
			Bids:       []Level{{Price: 98, Quantity: 1}, {Price: 100, Quantity: 1}, {Price: 99, Quantity: 1}},
			Asks:       []Level{{Price: 103, Quantity: 1}, {Price: 101, Quantity: 1}, {Price: 102, Quantity: 1}},
		},
	}
	snap, prob := FuseDepth("BTCUSDT", 100_000, inputs, FusionMerge, 30_000)
	if prob != nil {
		t.Fatalf("fuse failed: %v", prob)
	}
	for i := 1; i < len(snap.Bids); i++ {
		if snap.Bids[i].PriceFP > snap.Bids[i-1].PriceFP {
			t.Fatalf("bids not sorted desc at index %d", i)
		}
	}
	for i := 1; i < len(snap.Asks); i++ {
		if snap.Asks[i].PriceFP < snap.Asks[i-1].PriceFP {
			t.Fatalf("asks not sorted asc at index %d", i)
		}
	}
}

func TestFuseDepth_ValidationRejectsInvalidMode(t *testing.T) {
	_, prob := FuseDepth("BTCUSDT", 100_000, nil, "invalid", 30_000)
	if prob == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestFuseDepth_ValidationRejectsEmptyInstrument(t *testing.T) {
	_, prob := FuseDepth("", 100_000, nil, FusionMerge, 30_000)
	if prob == nil {
		t.Fatal("expected error for empty instrument")
	}
}

func TestDeriveFusionConfidence_AllFresh(t *testing.T) {
	sources := []SourceEntry{
		{Venue: "A", IsStale: false},
		{Venue: "B", IsStale: false},
		{Venue: "C", IsStale: false},
	}
	c := DeriveFusionConfidence(sources)
	if c != 1.0 {
		t.Fatalf("confidence=%f want=1.0", c)
	}
}

func TestDeriveFusionConfidence_OneFresh(t *testing.T) {
	sources := []SourceEntry{
		{Venue: "A", IsStale: false},
		{Venue: "B", IsStale: true},
		{Venue: "C", IsStale: true},
	}
	c := DeriveFusionConfidence(sources)
	// 1/3 * 0.7 ≈ 0.233
	if c < 0.2 || c > 0.3 {
		t.Fatalf("confidence=%f want~0.233", c)
	}
}

func TestDeriveFusionConfidence_Empty(t *testing.T) {
	c := DeriveFusionConfidence(nil)
	if c != 0 {
		t.Fatalf("confidence=%f want=0", c)
	}
}

func TestDeriveFeatureTags_Sorted(t *testing.T) {
	sources := []SourceEntry{
		{Venue: "A", IsStale: true},
		{Venue: "B", IsStale: false},
	}
	tags := DeriveFeatureTags(0.4, sources, 60, true)
	for i := 1; i < len(tags); i++ {
		if tags[i] < tags[i-1] {
			t.Fatalf("tags not sorted at index %d: %v", i, tags)
		}
	}
}

func TestBuildSourceMix_EqualWeights(t *testing.T) {
	venues := map[string]int64{"BINANCE": 99_000, "BYBIT": 99_500}
	mix := BuildSourceMix(venues, 100_000, 30_000)
	if len(mix) != 2 {
		t.Fatalf("mix len=%d want=2", len(mix))
	}
	for _, s := range mix {
		if s.WeightPct != 50 {
			t.Fatalf("weight_pct=%f want=50", s.WeightPct)
		}
	}
	if mix[0].Venue != "BINANCE" || mix[1].Venue != "BYBIT" {
		t.Fatalf("not sorted: %s,%s", mix[0].Venue, mix[1].Venue)
	}
}

func TestFusionMeta_Validate(t *testing.T) {
	valid := FusionMeta{
		Reason:      "cross_venue_merge",
		Confidence:  0.9,
		SourceMix:   []SourceEntry{{Venue: "A", WeightPct: 100, LastSeenMs: 99_000}},
		Staleness:   StalenessReport{FreshCount: 1},
		FeatureTags: []string{"high_confidence"},
	}
	if p := valid.Validate(); p != nil {
		t.Fatalf("validate failed: %v", p)
	}

	invalid := valid
	invalid.Confidence = 1.5
	if p := invalid.Validate(); p == nil {
		t.Fatal("expected validation error for confidence > 1")
	}

	invalid2 := valid
	invalid2.SourceMix = nil
	if p := invalid2.Validate(); p == nil {
		t.Fatal("expected validation error for empty source_mix")
	}
}
