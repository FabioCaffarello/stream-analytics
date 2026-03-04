package contracts

// AggregationCandleClosedV1 is the shared wire DTO for aggregation.candle v1.
type AggregationCandleClosedV1 struct {
	Candle AggregationCandleV1
}

// AggregationCandleV1 is the JSON wire DTO for closed candle payloads.
type AggregationCandleV1 struct {
	Venue         string
	Instrument    string
	Timeframe     string
	WindowStartTs int64
	WindowEndTs   int64
	Open          float64
	High          float64
	Low           float64
	ClosePrice    float64
	Volume        float64
	BuyVolume     float64
	SellVolume    float64
	TradeCount    int64
	SeqFirst      int64
	SeqLast       int64
	IsClosed      bool
}

// AggregationStatsWindowClosedV1 is the shared wire DTO for aggregation.stats v1.
type AggregationStatsWindowClosedV1 struct {
	Stats AggregationStatsWindowV1
}

// AggregationStatsWindowV1 is the JSON wire DTO for closed stats payloads.
type AggregationStatsWindowV1 struct {
	Venue           string
	Instrument      string
	Timeframe       string
	WindowStartTs   int64
	WindowEndTs     int64
	LiqBuyVolume    float64
	LiqSellVolume   float64
	LiqTotalVolume  float64
	LiqCount        int64
	MarkPriceOpen   float64
	MarkPriceHigh   float64
	MarkPriceLow    float64
	MarkPriceClose  float64
	FundingRateAvg  float64
	FundingRateLast float64
	SeqFirst        int64
	SeqLast         int64
	IsClosed        bool
}

// AggregationSnapshotV1 is the shared wire DTO for aggregation.snapshot v1.
type AggregationSnapshotV1 struct {
	Venue      string
	Instrument string
	Seq        int64
	Bids       []AggregationOrderBookLevelV1
	Asks       []AggregationOrderBookLevelV1
}

// AggregationOrderBookLevelV1 is a single price level in the wire DTO.
type AggregationOrderBookLevelV1 struct {
	Price    float64
	Quantity float64
}

// AggregationOrderBookInconsistencyV1 is the shared wire DTO for aggregation.orderbook_inconsistency v1.
type AggregationOrderBookInconsistencyV1 struct {
	Venue      string
	Instrument string
	Seq        int64
	Reason     string
}

// AggregationCrossVenueBookVenueLevelV1 is a single venue level in the wire DTO.
type AggregationCrossVenueBookVenueLevelV1 struct {
	Venue   string `json:"venue"`
	PriceFP int64  `json:"price_fp"`
	SizeFP  int64  `json:"size_fp"`
}

// AggregationCrossVenueBookSnapshotV1 is the shared wire DTO for aggregation.cross_venue_book v1.
type AggregationCrossVenueBookSnapshotV1 struct {
	Instrument         string                                  `json:"instrument"`
	TsServerMs         int64                                   `json:"ts_server_ms"`
	BestBids           []AggregationCrossVenueBookVenueLevelV1 `json:"best_bids"`
	BestAsks           []AggregationCrossVenueBookVenueLevelV1 `json:"best_asks"`
	GlobalSpreadBPS    float64                                 `json:"global_spread_bps"`
	VenueDivergenceBPS float64                                 `json:"venue_divergence_bps"`
}
