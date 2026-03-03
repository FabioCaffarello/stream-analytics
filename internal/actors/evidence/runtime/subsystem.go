package evidenceruntime

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anthdm/hollywood/actor"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	evidenceapp "github.com/market-raccoon/internal/core/evidence/app"
	"github.com/market-raccoon/internal/core/evidence/domain"
	marketdomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
)

const (
	envelopeTypeTrade     = "marketdata.trade"
	envelopeTypeBookDelta = "marketdata.bookdelta"
)

type evidenceEnvelopeMsg struct {
	Envelope envelope.Envelope
}

// SubsystemConfig configures the Evidence subsystem actor.
type SubsystemConfig struct {
	Logger     *slog.Logger
	EnvelopeCh <-chan envelope.Envelope
	Engine     *evidenceapp.EvidenceEngine
	RouterPID  *actor.PID // delivery router to publish evidence envelopes
}

// SubsystemActor owns the evidence engine lifecycle.
type SubsystemActor struct {
	cfg        SubsystemConfig
	logger     *slog.Logger
	engine     *actor.Engine
	selfPID    *actor.PID
	consumeCtx context.Context
	cancel     context.CancelFunc
}

// NewSubsystemActor creates the evidence subsystem actor producer.
func NewSubsystemActor(cfg SubsystemConfig) actor.Producer {
	return func() actor.Receiver {
		return &SubsystemActor{cfg: cfg}
	}
}

func (s *SubsystemActor) Receive(c *actor.Context) {
	s.ensureDefaults(c)

	switch msg := c.Message().(type) {
	case actor.Initialized:
	case actor.Started:
		s.onStarted(c)
	case actor.Stopped:
		s.onStopped()
	case evidenceEnvelopeMsg:
		s.processEnvelope(msg.Envelope)
	case actorruntime.ChildFailed:
		if c.Parent() != nil {
			c.Send(c.Parent(), msg)
		}
	default:
		s.logger.Warn("evidence subsystem: unknown message", "type", fmt.Sprintf("%T", msg))
	}
}

func (s *SubsystemActor) ensureDefaults(c *actor.Context) {
	if s.logger == nil {
		if s.cfg.Logger != nil {
			s.logger = s.cfg.Logger
		} else {
			s.logger = slog.Default()
		}
	}
	if s.engine == nil && c != nil {
		s.engine = c.Engine()
		s.selfPID = c.PID()
	}
}

func (s *SubsystemActor) onStarted(_ *actor.Context) {
	s.logger.Info("evidence subsystem started")
	if s.cfg.EnvelopeCh != nil && s.engine != nil && s.selfPID != nil {
		s.consumeCtx, s.cancel = context.WithCancel(context.Background())
		go s.consumeLoop()
	}
}

func (s *SubsystemActor) onStopped() {
	if s.cancel != nil {
		s.cancel()
	}
	s.logger.Info("evidence subsystem stopped")
}

func (s *SubsystemActor) consumeLoop() {
	for {
		select {
		case <-s.consumeCtx.Done():
			return
		case env, ok := <-s.cfg.EnvelopeCh:
			if !ok {
				return
			}
			s.engine.Send(s.selfPID, evidenceEnvelopeMsg{Envelope: env})
		}
	}
}

func (s *SubsystemActor) processEnvelope(env envelope.Envelope) {
	ruleEvent, ok := s.toRuleEvent(env)
	if !ok {
		return
	}

	evidenceEvents := s.cfg.Engine.OnEvent(ruleEvent)

	for i := range evidenceEvents {
		s.emitEvidence(env, evidenceEvents[i])
	}
}

func (s *SubsystemActor) toRuleEvent(env envelope.Envelope) (domain.RuleEvent, bool) {
	base := domain.RuleEvent{
		Venue:      env.Venue,
		Instrument: env.Instrument,
		TsServer:   env.TsIngest,
		Seq:        env.Seq,
	}

	switch env.Type {
	case envelopeTypeTrade:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			return domain.RuleEvent{}, false
		}
		trade, ok := decoded.(marketdomain.TradeTickV1)
		if !ok {
			return domain.RuleEvent{}, false
		}
		base.Kind = domain.EventKindTrade
		base.TradePrice = trade.Price
		base.TradeSize = trade.Size
		base.TradeSide = trade.Side
		return base, true

	case envelopeTypeBookDelta:
		decoded, p := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
		if p != nil {
			return domain.RuleEvent{}, false
		}
		book, ok := decoded.(marketdomain.BookDeltaV1)
		if !ok {
			return domain.RuleEvent{}, false
		}
		base.Kind = domain.EventKindBook
		if len(book.Bids) > 0 {
			base.BestBid = book.Bids[0].Price
		}
		if len(book.Asks) > 0 {
			base.BestAsk = book.Asks[0].Price
		}
		base.BidDepth = sumDepth(book.Bids)
		base.AskDepth = sumDepth(book.Asks)
		base.BidLevels = len(book.Bids)
		base.AskLevels = len(book.Asks)
		return base, true

	default:
		return domain.RuleEvent{}, false
	}
}

func sumDepth(levels []marketdomain.PriceLevel) float64 {
	total := 0.0
	for _, l := range levels {
		total += l.Size
	}
	return total
}

func (s *SubsystemActor) emitEvidence(triggerEnv envelope.Envelope, ev domain.EvidenceEvent) {
	payload, p := codec.EncodePayload(domain.MicrostructureEvidenceType, domain.MicrostructureEvidenceVersion, envelope.ContentTypeJSON, ev)
	if p != nil {
		s.logger.Warn("evidence: failed to encode evidence payload", "err", p.Message)
		return
	}

	evidenceEnv := envelope.Envelope{
		Type:        domain.MicrostructureEvidenceType,
		Version:     domain.MicrostructureEvidenceVersion,
		Venue:       ev.Venue,
		Instrument:  ev.Symbol,
		TsIngest:    ev.TsServer,
		Seq:         ev.SeqTrigger,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
	}

	if s.cfg.RouterPID != nil && s.engine != nil {
		s.engine.Send(s.cfg.RouterPID, deliveryruntime.DeliverEnvelope{Envelope: evidenceEnv})
	}
}
