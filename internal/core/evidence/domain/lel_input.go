package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/naming"
)

// LELEventKind discriminates LEL input source.
type LELEventKind int

const (
	LELEventKindSnapshot LELEventKind = iota
	LELEventKindTape
)

// LELEvent is the canonical flat input struct for LEL v1.
type LELEvent struct {
	Kind     LELEventKind
	Venue    string
	Symbol   string
	StreamID string
	TsServer int64
	Seq      int64

	// Snapshot fields.
	BestBid   float64
	BestAsk   float64
	SpreadBPS float64
	BidDepth  float64
	AskDepth  float64
	BidLevels int
	AskLevels int

	// Tape fields.
	TradeCount    int64
	BuyVolume     float64
	SellVolume    float64
	TotalVolume   float64
	VwapPrice     float64
	MaxPrice      float64
	MinPrice      float64
	Rate          float64
	Imbalance     float64
	IsBurst       bool
	WindowStartTs int64
	WindowEndTs   int64
}

// CanonicalVenue returns normalized venue.
func (e LELEvent) CanonicalVenue() string {
	return naming.CanonicalVenue(e.Venue)
}

// CanonicalSymbol returns normalized symbol.
func (e LELEvent) CanonicalSymbol() string {
	return naming.CanonicalInstrument(e.Symbol)
}

// StreamKey returns deterministic stream identity for rule-local state.
func (e LELEvent) StreamKey() string {
	if streamID := strings.TrimSpace(e.StreamID); streamID != "" {
		return streamID
	}
	return e.CanonicalVenue() + "|" + e.CanonicalSymbol()
}

// LELRule is the deterministic strategy interface for LEL v1 rules.
type LELRule interface {
	Name() string
	OnEvent(event LELEvent) []LiquidityEvidence
	StreamCount() int
	Reset()
	EvictStream(key string)
}
