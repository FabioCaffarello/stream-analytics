package app

import (
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/core/marketdata/domain"
	sharedhash "github.com/FabioCaffarello/stream-analytics/internal/shared/hash"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/naming"
)

func BuildMarkPriceDedupKey(
	venue string,
	instrument string,
	version int,
	tsExchange int64,
	tsIngest int64,
	payload domain.MarkPriceTickV1,
	sourceIdempotency string,
) string {
	base := lmDedupBaseHasher(markPriceEventType, version, venue, instrument)
	if src := strings.TrimSpace(sourceIdempotency); src != "" {
		return base.String(src).Hex()
	}
	return base.
		Float64(payload.MarkPrice).
		Float64(payload.IndexPrice).
		Float64(payload.FundingRate).
		Int64(payload.Timestamp).
		Int64(tsExchange).
		Int64(tsIngest).
		Hex()
}

func BuildLiquidationDedupKey(
	venue string,
	instrument string,
	version int,
	tsExchange int64,
	tsIngest int64,
	payload domain.LiquidationTickV1,
	sourceIdempotency string,
) string {
	base := lmDedupBaseHasher(liquidationEventType, version, venue, instrument)
	if src := strings.TrimSpace(sourceIdempotency); src != "" {
		return base.String(src).Hex()
	}
	return base.
		String(strings.ToLower(strings.TrimSpace(payload.Side))).
		Float64(payload.Price).
		Float64(payload.Size).
		Int64(payload.Timestamp).
		Int64(tsExchange).
		Int64(tsIngest).
		Hex()
}

func lmDedupBaseHasher(eventType string, version int, venue, instrument string) sharedhash.FieldHasher {
	return sharedhash.NewFieldHasher().
		String(eventType).
		Int(version).
		String(naming.CanonicalVenue(venue)).
		String(naming.CanonicalInstrument(instrument))
}
