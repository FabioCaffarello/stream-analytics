// Package aggruntime contains the Aggregation subsystem actor, which bridges
// the event bus with the core aggregation use cases.
//
// Responsibilities:
//   - Subscribe to an envelope channel (from InMemoryBus or any source).
//   - Route incoming envelopes by type to the appropriate use case.
//   - Report fatal bus failures to the Guardian as runtime.ChildFailed.
//
// v1 routing table:
//
//	"marketdata.bookdelta" v1 → UpdateOrderBookFromEvents
//	"marketdata.raw"       v1 → skip (no structured payload)
//	anything else              → log warn + skip
package aggruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
)

const (
	typeBookDelta = "marketdata.bookdelta"
	typeRaw       = "marketdata.raw"
)

// busClosedMsg is sent by the consume goroutine when the envelope channel
// is closed.  It signals the actor to report a fatal failure to Guardian.
type busClosedMsg struct{}

// ProcessorConfig configures the ProcessorSubsystemActor.
type ProcessorConfig struct {
	// Logger is used for structured logging.  Defaults to slog.Default().
	Logger *slog.Logger

	// EnvelopeCh is the source of envelopes to process.  Typically obtained
	// via InMemoryBus.Subscribe().  The actor owns this channel for its
	// lifetime; it must not be shared with other actors.
	EnvelopeCh <-chan envelope.Envelope

	// UpdateBook is the aggregation use case for order book updates.
	// Required when routing BookDelta envelopes.
	UpdateBook *aggapp.UpdateOrderBookFromEvents
}

// ProcessorSubsystemActor consumes envelopes from a channel and dispatches
// them to core aggregation use cases.
//
// Message protocol (received):
//   - actor.Started    — starts the envelope consume goroutine.
//   - actor.Stopped    — cancels the goroutine.
//   - envelope.Envelope — routes to the appropriate use case.
//   - busClosedMsg      — signals channel closure → ChildFailed to Guardian.
type ProcessorSubsystemActor struct {
	cfg        ProcessorConfig
	logger     *slog.Logger
	engine     *actor.Engine
	selfPID    *actor.PID
	stopCancel context.CancelFunc
}

// NewProcessorSubsystemActor returns a hollywood actor.Producer for the
// ProcessorSubsystemActor using the given config.
func NewProcessorSubsystemActor(cfg ProcessorConfig) actor.Producer {
	return func() actor.Receiver {
		return &ProcessorSubsystemActor{cfg: cfg}
	}
}

// Receive handles actor messages.
func (p *ProcessorSubsystemActor) Receive(c *actor.Context) {
	p.ensureDefaults()
	switch msg := c.Message().(type) {
	case actor.Initialized:
		// no-op; engine lifecycle preamble.
	case actor.Started:
		p.onStarted(c)
	case actor.Stopped:
		p.onStopped()
	case envelope.Envelope:
		p.handleEnvelope(c, msg)
	case busClosedMsg:
		p.handleBusClosed(c)
	default:
		p.logger.Warn("aggruntime: unknown message", "type", fmt.Sprintf("%T", msg))
	}
}

func (p *ProcessorSubsystemActor) ensureDefaults() {
	if p.logger == nil {
		if p.cfg.Logger != nil {
			p.logger = p.cfg.Logger
		} else {
			p.logger = slog.Default()
		}
	}
}

func (p *ProcessorSubsystemActor) onStarted(c *actor.Context) {
	p.selfPID = c.PID()
	p.engine = c.Engine()

	ctx, cancel := context.WithCancel(context.Background())
	p.stopCancel = cancel

	p.logger.Info("aggruntime: processor started")

	if p.cfg.EnvelopeCh == nil {
		p.logger.Debug("aggruntime: no envelope channel configured — processor idle")
		return
	}
	go p.consumeLoop(ctx)
}

func (p *ProcessorSubsystemActor) onStopped() {
	if p.stopCancel != nil {
		p.stopCancel()
	}
}

// consumeLoop runs in a goroutine and forwards envelopes to the actor's mailbox.
// It exits when ctx is cancelled (actor stopped) or the channel is closed.
func (p *ProcessorSubsystemActor) consumeLoop(ctx context.Context) {
	for {
		select {
		case env, ok := <-p.cfg.EnvelopeCh:
			if !ok {
				// Bus was closed; notify actor.
				p.engine.Send(p.selfPID, busClosedMsg{})
				return
			}
			p.engine.Send(p.selfPID, env)
		case <-ctx.Done():
			return
		}
	}
}

// handleEnvelope routes the envelope to the appropriate use case.
func (p *ProcessorSubsystemActor) handleEnvelope(c *actor.Context, env envelope.Envelope) {
	switch env.Type {
	case typeBookDelta:
		p.handleBookDelta(env)
	case typeRaw:
		p.logger.Debug("aggruntime: skipping raw envelope",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
		)
	default:
		p.logger.Warn("aggruntime: unhandled envelope type",
			"type", env.Type,
			"version", env.Version,
		)
	}
}

// handleBookDelta decodes a BookDeltaV1 payload and calls UpdateOrderBook.
func (p *ProcessorSubsystemActor) handleBookDelta(env envelope.Envelope) {
	if p.cfg.UpdateBook == nil {
		p.logger.Warn("aggruntime: no UpdateBook use case configured — dropping bookdelta")
		return
	}

	var delta mddomain.BookDeltaV1
	if prob := codec.UnmarshalPayload(env.Type, env.Version, env.Payload, &delta); prob != nil {
		p.logger.Warn("aggruntime: failed to decode bookdelta payload",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
			"code", prob.Code,
			"err", prob.Message,
		)
		return
	}

	req := aggapp.UpdateRequest{
		Venue:      env.Venue,
		Instrument: env.Instrument,
		Seq:        env.Seq,
		Bids:       toLevels(delta.Bids),
		Asks:       toLevels(delta.Asks),
	}

	res := p.cfg.UpdateBook.Execute(context.Background(), req)
	if res.IsFail() {
		prob := res.Problem()
		p.logger.Warn("aggruntime: UpdateOrderBook failed",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
			"code", prob.Code,
			"retryable", prob.Retryable,
		)
		return
	}

	resp := res.Value()
	p.logger.Debug("aggruntime: order book updated",
		"venue", env.Venue,
		"instrument", env.Instrument,
		"seq", resp.Seq,
		"spread", resp.Spread,
	)
}

// handleBusClosed signals the Guardian that the envelope source is gone.
func (p *ProcessorSubsystemActor) handleBusClosed(c *actor.Context) {
	p.logger.Warn("aggruntime: envelope channel closed unexpectedly")
	if c.Parent() == nil {
		return
	}
	c.Send(c.Parent(), actorruntime.ChildFailed{
		Subsystem: actorruntime.SubsystemAggregation,
		Kind:      "bus_closed",
		Err:       errors.New("envelope channel closed unexpectedly"),
	})
}

// toLevels maps marketdata PriceLevel slices to aggregation domain Level slices.
func toLevels(pls []mddomain.PriceLevel) []aggdomain.Level {
	if len(pls) == 0 {
		return nil
	}
	levels := make([]aggdomain.Level, len(pls))
	for i, pl := range pls {
		levels[i] = aggdomain.Level{
			Price:    aggdomain.Price(pl.Price),
			Quantity: aggdomain.Quantity(pl.Size),
		}
	}
	return levels
}
