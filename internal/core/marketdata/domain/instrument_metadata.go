package domain

import (
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// InstrumentMetadata is the strong identity object for exchange instruments.
type InstrumentMetadata struct {
	VenueSymbol     string
	CanonicalSymbol string
	BaseAsset       string
	QuoteAsset      string
	MarketType      MarketType
}

// NewInstrumentMetadata validates and builds InstrumentMetadata.
func NewInstrumentMetadata(canonicalSymbol, venueSymbol, marketType string) (InstrumentMetadata, *problem.Problem) {
	base, quote, p := ParseCanonicalPair(canonicalSymbol)
	if p != nil {
		return InstrumentMetadata{}, p
	}
	mt, p := NewMarketType(marketType)
	if p != nil {
		return InstrumentMetadata{}, p
	}
	vs := strings.ToUpper(strings.TrimSpace(venueSymbol))
	if vs == "" {
		return InstrumentMetadata{}, problem.WithDetail(
			problem.New(problem.ValidationFailed, "venue_symbol must not be empty"),
			"field", "venue_symbol",
		)
	}
	return InstrumentMetadata{
		VenueSymbol:     vs,
		CanonicalSymbol: base + "-" + quote,
		BaseAsset:       base,
		QuoteAsset:      quote,
		MarketType:      mt,
	}, nil
}
