// Package bus provides EventPublisher adapters for the stream-analytics event bus.
//
// v1 ships two adapters:
//   - LogPublisher: writes every envelope to a slog.Logger (development / debugging).
//   - InMemoryBus:  fan-out to in-process subscribers via a buffered channel (test / processor v1).
//
// Both satisfy the port signature:
//
//	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
//
// No explicit import of core/marketdata/ports is required; callers verify the
// interface assignment at the use site.
package bus

import (
	"context"
	"log/slog"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// LogPublisher logs every published envelope via slog at DEBUG level.
// It is safe for concurrent use.
type LogPublisher struct {
	logger *slog.Logger
}

// NewLogPublisher creates a LogPublisher that writes to the given logger.
// If logger is nil, slog.Default() is used.
func NewLogPublisher(logger *slog.Logger) *LogPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogPublisher{logger: logger}
}

// Publish logs the envelope and returns nil (never fails).
func (p *LogPublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.logger.Debug("bus: published envelope",
		"type", env.Type,
		"version", env.Version,
		"venue", env.Venue,
		"instrument", env.Instrument,
		"seq", env.Seq,
		"ts_ingest", env.TsIngest,
		"topic", env.TopicKey(),
		"idempotency_key", env.IdempotencyKey,
	)
	return nil
}
