package domain

// TradeTickV1 is the V1 schema for a single trade event.
type TradeTickV1 struct {
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	Side      string  `json:"side"` // "buy" | "sell"
	TradeID   string  `json:"trade_id"`
	Timestamp int64   `json:"timestamp_ms"`
}

// BookDeltaV1 is the V1 schema for an order book delta (incremental update).
type BookDeltaV1 struct {
	Bids      []PriceLevel `json:"bids"` // [price, size]; size=0 means remove
	Asks      []PriceLevel `json:"asks"`
	FirstID   int64        `json:"first_update_id,omitempty"`      // Binance U
	FinalID   int64        `json:"final_update_id,omitempty"`      // Binance u
	PrevFinal int64        `json:"prev_final_update_id,omitempty"` // Binance pu (when present)
	Timestamp int64        `json:"timestamp_ms"`
}

// PriceLevel represents a [price, size] entry in the order book.
type PriceLevel struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

// MarkPriceTickV1 is the V1 schema for a mark price update.
type MarkPriceTickV1 struct {
	MarkPrice   float64 `json:"mark_price"`
	IndexPrice  float64 `json:"index_price"`
	FundingRate float64 `json:"funding_rate"`
	Timestamp   int64   `json:"timestamp_ms"`
}

// LiquidationTickV1 is the V1 schema for a liquidation event.
type LiquidationTickV1 struct {
	Side      string  `json:"side"` // "buy" | "sell"
	Price     float64 `json:"price"`
	Size      float64 `json:"size"`
	Timestamp int64   `json:"timestamp_ms"`
}
