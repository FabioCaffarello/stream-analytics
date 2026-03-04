package domain

// EventKind discriminates the type of market event fed to rules.
type EventKind int

const (
	EventKindTrade EventKind = iota
	EventKindBook
	EventKindCandle
)

// RuleEvent is the flat input struct for all evidence rules.
// Only fields matching Kind are populated; rules must check Kind before access.
type RuleEvent struct {
	Kind       EventKind
	Venue      string
	Instrument string
	TsServer   int64 // deterministic (from envelope, not wall clock)
	Seq        int64

	// Trade (Kind == EventKindTrade)
	TradePrice float64
	TradeSize  float64
	TradeSide  string // "buy"|"sell"

	// Book (Kind == EventKindBook)
	BestBid   float64
	BestAsk   float64
	BidDepth  float64 // sum of top-N quantities
	AskDepth  float64
	BidLevels int
	AskLevels int

	// Candle (Kind == EventKindCandle)
	CandleOpen        float64
	CandleClose       float64
	CandleHigh        float64
	CandleLow         float64
	CandleVolume      float64
	CandleBuyVol      float64
	CandleSellVol     float64
	CandleWindowStart int64
	CandleWindowEnd   int64
	CandleTimeframe   string
}

// StreamKey returns the canonical partition key for per-stream state.
func (e RuleEvent) StreamKey() string {
	return e.Venue + "|" + e.Instrument
}

// EvidenceRule is the strategy interface for deterministic microstructure detection.
type EvidenceRule interface {
	// Name returns the stable rule identifier (matches an EvidenceKind).
	Name() string
	// OnEvent processes a market event and returns zero or more evidence emissions.
	OnEvent(event RuleEvent) []EvidenceEvent
	// StreamCount returns the number of active per-stream state entries.
	StreamCount() int
	// Reset clears all per-stream state.
	Reset()
	// EvictStream removes state for a specific stream key.
	EvictStream(key string)
}
