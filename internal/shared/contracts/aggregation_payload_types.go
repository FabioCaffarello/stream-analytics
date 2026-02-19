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
