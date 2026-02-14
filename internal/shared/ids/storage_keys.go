package ids

import (
	"strconv"
	"strings"

	"github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/naming"
)

const (
	aggregationSnapshotSubject = "aggregation.snapshot.v1"
	heatmapSnapshotSubject     = "insights.heatmap_snapshot.v1"
)

// DeterministicStorageWriteKey returns a stable key for storage write idempotency.
// The key is canonicalized across venue/instrument casing and separator variants.
func DeterministicStorageWriteKey(subject, venue, instrument string, seq int64, sourceIdempotencyKey string) string {
	return hash.HashFields(
		naming.NormalizeEventType(subject),
		naming.CanonicalVenue(venue),
		naming.CanonicalInstrument(instrument),
		strconv.FormatInt(seq, 10),
		sourceIdempotencyKey,
	)
}

// AggregationSnapshotWriteKey builds the deterministic idempotency key used by
// hot/cold storage writers for aggregation snapshot persistence.
func AggregationSnapshotWriteKey(venue, instrument string, seq int64, sourceIdempotencyKey string) string {
	return DeterministicStorageWriteKey(aggregationSnapshotSubject, venue, instrument, seq, sourceIdempotencyKey)
}

// HeatmapArtifactWriteKey builds the deterministic idempotency key for
// heatmap artifacts persisted in hot/cold paths.
func HeatmapArtifactWriteKey(venue, instrument, timeframe string, windowStartTs int64, sourceIdempotencyKey string) string {
	return hash.HashFields(
		naming.NormalizeEventType(heatmapSnapshotSubject),
		naming.CanonicalVenue(venue),
		naming.CanonicalInstrument(instrument),
		strconv.FormatInt(windowStartTs, 10),
		strings.ToLower(strings.TrimSpace(timeframe)),
		sourceIdempotencyKey,
	)
}
