package contracts

// FusionSourceEntryV1 is the wire DTO for one venue's contribution.
type FusionSourceEntryV1 struct {
	Venue      string  `json:"venue"`
	WeightPct  float64 `json:"weight_pct"`
	LastSeenMs int64   `json:"last_seen_ms"`
	IsStale    bool    `json:"is_stale"`
}

// FusionStalenessReportV1 is the wire DTO for staleness summary.
type FusionStalenessReportV1 struct {
	FreshCount int   `json:"fresh_count"`
	StaleCount int   `json:"stale_count"`
	OldestMs   int64 `json:"oldest_ms"`
	NewestMs   int64 `json:"newest_ms"`
}

// FusionMetaV1 is the wire DTO for fusion evidence metadata.
type FusionMetaV1 struct {
	Reason      string                  `json:"reason"`
	Confidence  float64                 `json:"confidence"`
	SourceMix   []FusionSourceEntryV1   `json:"source_mix"`
	Staleness   FusionStalenessReportV1 `json:"staleness"`
	FeatureTags []string                `json:"feature_tags"`
}

// FusedLevelV1 is the wire DTO for one fused depth level.
type FusedLevelV1 struct {
	PriceFP int64    `json:"price_fp"`
	SizeFP  int64    `json:"size_fp"`
	Venues  []string `json:"venues"`
}

// FusedDepthSnapshotV1 is the wire DTO for fused orderbook depth.
type FusedDepthSnapshotV1 struct {
	Instrument      string         `json:"instrument"`
	TsServerMs      int64          `json:"ts_server_ms"`
	Mode            string         `json:"mode"`
	Bids            []FusedLevelV1 `json:"bids"`
	Asks            []FusedLevelV1 `json:"asks"`
	SourceVenues    []string       `json:"source_venues"`
	GlobalSpreadBPS float64        `json:"global_spread_bps"`
	Meta            FusionMetaV1   `json:"meta"`
}

// FusedVenueContributionV1 is the wire DTO for one venue's volume contribution.
type FusedVenueContributionV1 struct {
	Venue     string  `json:"venue"`
	Volume    float64 `json:"volume"`
	WeightPct float64 `json:"weight_pct"`
}

// FusedVolumeProfileBucketV1 is the wire DTO for one fused VPVR bucket.
type FusedVolumeProfileBucketV1 struct {
	PriceLow    float64                    `json:"price_low"`
	PriceHigh   float64                    `json:"price_high"`
	BuyVolume   float64                    `json:"buy_volume"`
	SellVolume  float64                    `json:"sell_volume"`
	TotalVolume float64                    `json:"total_volume"`
	VenueMix    []FusedVenueContributionV1 `json:"venue_mix"`
}

// FusedVolumeProfileSnapshotV1 is the wire DTO for fused VPVR.
type FusedVolumeProfileSnapshotV1 struct {
	Instrument    string                       `json:"instrument"`
	Timeframe     string                       `json:"timeframe"`
	WindowStartTs int64                        `json:"window_start_ts"`
	WindowEndTs   int64                        `json:"window_end_ts"`
	Mode          string                       `json:"mode"`
	Buckets       []FusedVolumeProfileBucketV1 `json:"buckets"`
	POCPrice      float64                      `json:"poc_price"`
	ValueAreaLow  float64                      `json:"value_area_low"`
	ValueAreaHigh float64                      `json:"value_area_high"`
	SourceVenues  []string                     `json:"source_venues"`
	Meta          FusionMetaV1                 `json:"meta"`
}

// FusedHeatmapCellV1 is the wire DTO for one fused heatmap cell.
type FusedHeatmapCellV1 struct {
	PriceBucketLow  float64                    `json:"price_bucket_low"`
	PriceBucketHigh float64                    `json:"price_bucket_high"`
	SizeBucket      string                     `json:"size_bucket"`
	BidLiquidity    float64                    `json:"bid_liquidity"`
	AskLiquidity    float64                    `json:"ask_liquidity"`
	TradeVolume     float64                    `json:"trade_volume"`
	Samples         int64                      `json:"samples"`
	VenueMix        []FusedVenueContributionV1 `json:"venue_mix"`
}

// FusedHeatmapArtifactV1 is the wire DTO for fused heatmap.
type FusedHeatmapArtifactV1 struct {
	Instrument    string               `json:"instrument"`
	Timeframe     string               `json:"timeframe"`
	WindowStartTs int64                `json:"window_start_ts"`
	WindowEndTs   int64                `json:"window_end_ts"`
	Mode          string               `json:"mode"`
	Cells         []FusedHeatmapCellV1 `json:"cells"`
	SourceVenues  []string             `json:"source_venues"`
	Meta          FusionMetaV1         `json:"meta"`
}
