package app

import (
	"strconv"
	"strings"

	"github.com/market-raccoon/internal/core/marketdata/domain"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/naming"
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
	base := lmDedupBase(markPriceEventType, version, venue, instrument)
	if src := strings.TrimSpace(sourceIdempotency); src != "" {
		return sharedhash.HashFieldsFast(append(base, src)...)
	}
	return sharedhash.HashFieldsFast(append(base,
		strconv.FormatFloat(payload.MarkPrice, 'f', -1, 64),
		strconv.FormatFloat(payload.IndexPrice, 'f', -1, 64),
		strconv.FormatFloat(payload.FundingRate, 'f', -1, 64),
		strconv.FormatInt(payload.Timestamp, 10),
		strconv.FormatInt(tsExchange, 10),
		strconv.FormatInt(tsIngest, 10),
	)...)
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
	base := lmDedupBase(liquidationEventType, version, venue, instrument)
	if src := strings.TrimSpace(sourceIdempotency); src != "" {
		return sharedhash.HashFieldsFast(append(base, src)...)
	}
	return sharedhash.HashFieldsFast(append(base,
		strings.ToLower(strings.TrimSpace(payload.Side)),
		strconv.FormatFloat(payload.Price, 'f', -1, 64),
		strconv.FormatFloat(payload.Size, 'f', -1, 64),
		strconv.FormatInt(payload.Timestamp, 10),
		strconv.FormatInt(tsExchange, 10),
		strconv.FormatInt(tsIngest, 10),
	)...)
}

func lmDedupBase(eventType string, version int, venue, instrument string) []string {
	return []string{
		eventType,
		strconv.Itoa(version),
		naming.CanonicalVenue(venue),
		naming.CanonicalInstrument(instrument),
	}
}
