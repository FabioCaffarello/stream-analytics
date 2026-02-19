package domain

// TradeTickV1 is the V1 schema for a single trade event.
type TradeTickV1 struct {
	Price     float64
	Size      float64
	Side      string // "buy" | "sell"
	TradeID   string
	Timestamp int64
}

// BookDeltaV1 is the V1 schema for an order book delta (incremental update).
type BookDeltaV1 struct {
	Bids       []PriceLevel // [price, size]; size=0 means remove
	Asks       []PriceLevel
	FirstID    int64 // Binance U
	FinalID    int64 // Binance u
	PrevFinal  int64 // Binance pu (when present)
	Timestamp  int64
	IsSnapshot bool // true when the message is a full L2 snapshot (not incremental)
}

// PriceLevel represents a [price, size] entry in the order book.
type PriceLevel struct {
	Price float64
	Size  float64
}

// MarkPriceTickV1 is the V1 schema for a mark price update.
type MarkPriceTickV1 struct {
	MarkPrice   float64
	IndexPrice  float64
	FundingRate float64
	Timestamp   int64
}

// LiquidationTickV1 is the V1 schema for a liquidation event.
type LiquidationTickV1 struct {
	Side      string // "buy" | "sell"
	Price     float64
	Size      float64
	Timestamp int64
}
