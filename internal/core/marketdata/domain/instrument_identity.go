package domain

import (
	"strings"

	"github.com/market-raccoon/internal/shared/problem"
)

// InstrumentIdentity keeps canonical and venue-native identity details.
//
// Canonical format is "BASE-QUOTE" (e.g. "BTC-USDT").
type InstrumentIdentity struct {
	Canonical   string
	Base        string
	Quote       string
	VenueSymbol string
	MarketType  MarketType
}

// ParseCanonicalPair parses canonical instrument string ("BASE-QUOTE").
func ParseCanonicalPair(raw string) (base string, quote string, p *problem.Problem) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.NewReplacer("/", "-", "_", "-", ".", "-").Replace(s)

	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return "", "", problem.Newf(problem.ValidationFailed, "canonical instrument must be BASE-QUOTE, got %q", raw)
	}
	base = strings.TrimSpace(parts[0])
	quote = strings.TrimSpace(parts[1])
	if base == "" || quote == "" {
		return "", "", problem.Newf(problem.ValidationFailed, "canonical instrument has empty base or quote, got %q", raw)
	}
	return base, quote, nil
}

// NewInstrumentIdentity builds InstrumentIdentity from canonical instrument.
func NewInstrumentIdentity(canonical, venueSymbol, marketType string) (InstrumentIdentity, *problem.Problem) {
	meta, p := NewInstrumentMetadata(canonical, venueSymbol, marketType)
	if p != nil {
		return InstrumentIdentity{}, p
	}
	return InstrumentIdentity{
		Canonical:   meta.CanonicalSymbol,
		Base:        meta.BaseAsset,
		Quote:       meta.QuoteAsset,
		VenueSymbol: meta.VenueSymbol,
		MarketType:  meta.MarketType,
	}, nil
}
