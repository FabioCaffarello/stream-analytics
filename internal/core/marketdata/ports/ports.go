// Package ports defines the secondary (driven) port interfaces for the
// marketdata bounded context. Only interfaces — no implementations.
package ports

import (
	"context"

	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

// Clock is re-exported from shared for convenience of the app layer.
// The app layer depends on this port, not directly on shared/clock.
type Clock = clock.Clock

// Sequencer assigns monotonic sequence numbers per (venue, instrument) stream.
// Implementations must be concurrency-safe.
type Sequencer interface {
	// Next returns the next sequence number for the given (venue, instrument) pair.
	// The returned value is guaranteed to be strictly greater than all previous
	// values for the same pair.
	Next(venue, instrument string) (int64, *problem.Problem)
}

// InstrumentCatalog provides reference data for instruments.
type InstrumentCatalog interface {
	// TickSize returns the minimum price increment for an instrument.
	TickSize(venue, instrument string) (float64, *problem.Problem)

	// LotSize returns the minimum trade size for an instrument.
	LotSize(venue, instrument string) (float64, *problem.Problem)

	// PriceGroup returns a logical grouping tag for the instrument
	// (e.g. "crypto-major", "crypto-defi") used by the insight engine.
	PriceGroup(venue, instrument string) (string, *problem.Problem)
}

// InstrumentMetadataProvider resolves canonical instrument identity and market typing.
type InstrumentMetadataProvider interface {
	GetInstrument(symbol string) (domain.InstrumentMetadata, *problem.Problem)
}

// DepthSnapshot is the canonical initial depth model used by book bootstrap.
type DepthSnapshot struct {
	LastUpdateID int64
	Bids         []domain.PriceLevel
	Asks         []domain.PriceLevel
}

// DepthSnapshotProvider fetches an exchange snapshot for a symbol.
type DepthSnapshotProvider interface {
	Snapshot(symbol string) (DepthSnapshot, *problem.Problem)
}

// EventPublisher publishes a fully-formed Envelope to the event bus.
// The topic is derived from envelope.TopicKey() by the implementation.
type EventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}
