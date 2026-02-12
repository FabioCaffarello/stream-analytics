// Package app contains the application use cases for the marketdata bounded context.
// It orchestrates domain objects and secondary ports; it contains no business rules.
package app

import (
	"context"

	"github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/core/marketdata/ports"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/result"
	"github.com/market-raccoon/internal/shared/validation"
)

const defaultDedupWindowSize = 1024

// IngestRequest carries the raw ingest inputs from an adapter or actor.
type IngestRequest struct {
	Venue      string
	Instrument string
	EventType  string
	Version    int
	TsExchange int64 // Unix ms from exchange (advisory)
	// IdempotencyKey is an optional stable dedup key from the source stream.
	// When empty, the aggregate falls back to deterministic key derivation.
	IdempotencyKey string
	Payload        any // typed domain payload (e.g. TradeTickV1)
	Metadata       map[string]string
}

// IngestResponse is returned on success.
type IngestResponse struct {
	Published PublishedEvent
	Seq       int64
}

// PublishedEvent is the app-level output for a published envelope.
type PublishedEvent struct {
	Topic    string
	Envelope envelope.Envelope
}

// IngestConfig contains use-case level policies.
type IngestConfig struct {
	DedupWindowSize int
}

// IngestMarketData is the primary use case for ingesting a single market event.
//
// Steps:
//  1. Validate raw inputs
//  2. Normalize (via domain VOs)
//  3. Assign ts_ingest from clock
//  4. Assign seq from sequencer
//  5. Build envelope (domain aggregate)
//  6. Validate envelope
//  7. Publish via EventPublisher port
type IngestMarketData struct {
	clock       ports.Clock
	sequencer   ports.Sequencer
	publisher   ports.EventPublisher
	streams     map[domain.StreamID]*domain.InstrumentStream
	dedupWindow domain.DedupWindow
}

// NewIngestMarketData constructs the use case.
func NewIngestMarketData(
	clk ports.Clock,
	seq ports.Sequencer,
	pub ports.EventPublisher,
) *IngestMarketData {
	return NewIngestMarketDataWithConfig(clk, seq, pub, IngestConfig{DedupWindowSize: defaultDedupWindowSize})
}

// NewIngestMarketDataWithConfig constructs the use case with explicit config.
func NewIngestMarketDataWithConfig(
	clk ports.Clock,
	seq ports.Sequencer,
	pub ports.EventPublisher,
	cfg IngestConfig,
) *IngestMarketData {
	window, p := domain.NewDedupWindow(cfg.DedupWindowSize)
	if p != nil {
		// App-layer fallback: keep the aggregate free from hardcoded policy.
		window, _ = domain.NewDedupWindow(defaultDedupWindowSize)
	}

	return &IngestMarketData{
		clock:       clk,
		sequencer:   seq,
		publisher:   pub,
		streams:     make(map[domain.StreamID]*domain.InstrumentStream),
		dedupWindow: window,
	}
}

// Execute runs the ingest use case and returns a Result.
func (uc *IngestMarketData) Execute(ctx context.Context, req IngestRequest) result.Result[IngestResponse] {
	// 1. Validate raw inputs.
	if p := validation.Collect(
		validation.NonEmptyString("venue", req.Venue),
		validation.NonEmptyString("instrument", req.Instrument),
		validation.NonEmptyString("event_type", req.EventType),
		validation.PositiveInt("version", int64(req.Version)),
	); p != nil {
		return result.FailProblem[IngestResponse](p)
	}

	if req.Payload == nil {
		return result.FailProblem[IngestResponse](
			problem.New(problem.ValidationFailed, "payload must not be nil"),
		)
	}

	// 2. Normalize via domain VOs.
	eventType, p := domain.NewEventType(req.EventType)
	if p != nil {
		return result.FailProblem[IngestResponse](p)
	}
	version, p := domain.NewSchemaVersion(req.Version)
	if p != nil {
		return result.FailProblem[IngestResponse](p)
	}

	// 3. Get or create the stream aggregate (normalizes venue+instrument).
	stream, p := uc.getOrCreateStream(req.Venue, req.Instrument)
	if p != nil {
		return result.FailProblem[IngestResponse](p)
	}

	// 4. Assign ts_ingest from clock.
	tsIngest, p := domain.NewTimestamp(uc.clock.NowUnixMilli())
	if p != nil {
		return result.FailProblem[IngestResponse](p)
	}

	// TsExchange is advisory; use zero Timestamp if not provided or invalid.
	tsExchange := domain.Timestamp(req.TsExchange)
	if req.TsExchange <= 0 {
		tsExchange = tsIngest // fallback to ingest time
	}

	// 5. Assign seq from sequencer.
	seqNum, p := uc.sequencer.Next(stream.ID().Venue.String(), stream.ID().Instrument.String())
	if p != nil {
		return result.FailProblem[IngestResponse](p)
	}
	seq, p := domain.NewSequence(seqNum)
	if p != nil {
		return result.FailProblem[IngestResponse](p)
	}

	// 6. Build envelope (includes validate + dedup inside domain aggregate).
	env, p := stream.BuildEnvelope(eventType, version, tsExchange, tsIngest, seq, req.Payload, req.IdempotencyKey)
	if p != nil {
		return result.FailProblem[IngestResponse](p)
	}
	if len(req.Metadata) > 0 {
		meta := make(map[string]string, len(req.Metadata))
		for k, v := range req.Metadata {
			meta[k] = v
		}
		env.Meta = meta
	}

	// 7. Publish.
	if p := uc.publisher.Publish(ctx, env); p != nil {
		return result.FailProblem[IngestResponse](p)
	}

	published := PublishedEvent{
		Topic:    env.TopicKey(),
		Envelope: env,
	}

	return result.Ok(IngestResponse{
		Published: published,
		Seq:       env.Seq,
	})
}

// getOrCreateStream retrieves or lazily initialises an InstrumentStream aggregate.
func (uc *IngestMarketData) getOrCreateStream(rawVenue, rawInstrument string) (*domain.InstrumentStream, *problem.Problem) {
	// Build a temporary stream to get the canonical ID.
	tmpStream, p := domain.NewInstrumentStream(rawVenue, rawInstrument, uc.dedupWindow)
	if p != nil {
		return nil, p
	}
	id := tmpStream.ID()

	if existing, ok := uc.streams[id]; ok {
		return existing, nil
	}
	uc.streams[id] = tmpStream
	return tmpStream, nil
}
