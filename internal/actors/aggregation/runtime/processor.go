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
//	"marketdata.trade"     v1 → JoinCrossVenueTrades (when configured)
//	"marketdata.raw"       v1 → skip (no structured payload)
//	anything else              → log warn + skip
package aggruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	insightsapp "github.com/market-raccoon/internal/core/insights/app"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/policykit"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	typeBookDelta = "marketdata.bookdelta"
	typeTrade     = "marketdata.trade"
	typeRaw       = "marketdata.raw"

	reasonCodeDecodeFailed        = "DECODE_FAILED"
	reasonCodeValidationFailed    = "VALIDATION_FAILED"
	reasonCodeUnknownEventType    = "UNKNOWN_EVENT_TYPE"
	reasonCodeUnknownEventVersion = "UNKNOWN_EVENT_VERSION"

	metaKeyMarketType          = "instrument_market_type"
	metaKeySubjectPrefix       = "subject_prefix"
	snapshotDefaultContentType = envelope.ContentTypeJSON
)

// busClosedMsg is sent by the consume goroutine when the envelope channel
// is closed.  It signals the actor to report a fatal failure to Guardian.
type busClosedMsg struct{}

// EnvelopeProcessResult reports the processing outcome for one envelope.
type EnvelopeProcessResult struct {
	Envelope envelope.Envelope
	Problem  *problem.Problem
}

// EventPublisher publishes a canonical envelope to the configured bus adapter.
type EventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

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

	// JoinTrades is the optional insights use case for cross-venue trade joins.
	JoinTrades *insightsapp.JoinCrossVenueTrades

	// PublishEnvelope is required when JoinTrades is enabled.
	PublishEnvelope EventPublisher

	// SnapshotSubjectPrefix optionally overrides publish subject prefix for insight snapshots.
	SnapshotSubjectPrefix string

	// OnEnvelopeProcessed is an optional callback invoked after each envelope
	// processing attempt. It is used by runtime wiring (e.g. JetStream bridge)
	// to map processing outcomes into ack/nak/term dispositions.
	OnEnvelopeProcessed func(EnvelopeProcessResult)

	// PolicyKitEngine enables deterministic overload actions in the processor path.
	// When nil, policy actions are disabled (default).
	PolicyKitEngine policykit.Engine
	// PolicyKitBacklogCapacity normalizes backlog ratio for policy signals.
	PolicyKitBacklogCapacity int
	// PolicyKitResolver customizes subject category mapping.
	PolicyKitResolver policykit.CategoryResolver
}

// heartbeatInterval controls how often the processor emits an Info-level
// heartbeat log.  Every N envelopes processed, the actor logs aggregated
// counters so operators can prove the pipeline is alive without tailing
// Debug-level output.
const heartbeatInterval = 1000

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

	policyApplier *policykit.Applier
	policyLevels  map[string]policykit.Level

	// heartbeat state — pure runtime counters, not persisted.
	hbTotal         int64
	hbByType        map[string]int64
	hbLastSubject   string
	hbLastStreamSeq int64
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
		res := p.handleEnvelope(c, msg)
		metrics.IncProcessorProcessed(msg.Type, processorStatus(res))
		p.recordHeartbeat(msg)
		p.emitProcessedResult(msg, res)
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
	if p.cfg.PolicyKitBacklogCapacity <= 0 {
		p.cfg.PolicyKitBacklogCapacity = 1
	}
	if p.cfg.PolicyKitEngine != nil {
		if p.policyApplier == nil {
			p.policyApplier = policykit.NewApplier(p.cfg.PolicyKitResolver)
		}
		if p.policyLevels == nil {
			p.policyLevels = make(map[string]policykit.Level)
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
func (p *ProcessorSubsystemActor) handleEnvelope(_ *actor.Context, env envelope.Envelope) *problem.Problem {
	if adjusted, dropped := p.applyPolicyKit(env); dropped {
		return nil
	} else {
		env = adjusted
	}

	switch env.Type {
	case typeBookDelta:
		if env.Version != 1 {
			return unsupportedVersionProblem(env.Type, env.Version)
		}
		return p.handleBookDelta(env)
	case typeTrade:
		if env.Version != 1 {
			return unsupportedVersionProblem(env.Type, env.Version)
		}
		if p.cfg.JoinTrades == nil {
			return problem.WithDetail(
				problem.New(problem.ValidationFailed, "insights JoinTrades use case is not configured"),
				"reason_code", reasonCodeValidationFailed,
			)
		}
		return p.handleTrade(env)
	case typeRaw:
		if env.Version != 1 {
			return unsupportedVersionProblem(env.Type, env.Version)
		}
		p.logger.Debug("aggruntime: skipping raw envelope",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
		)
		return nil
	default:
		p.logger.Warn("aggruntime: unhandled envelope type", "type", env.Type, "version", env.Version)
		return unhandledTypeProblem(env.Type)
	}
}

func (p *ProcessorSubsystemActor) applyPolicyKit(env envelope.Envelope) (envelope.Envelope, bool) {
	if p.cfg.PolicyKitEngine == nil || p.policyApplier == nil {
		return env, false
	}
	started := time.Now()

	partition := env.Type + "|" + env.Venue + "|" + env.Instrument
	prev := p.policyLevels[partition]
	decision := p.cfg.PolicyKitEngine.Decide(prev, policykit.Signals{
		Backlog:    len(p.cfg.EnvelopeCh),
		BacklogCap: p.cfg.PolicyKitBacklogCapacity,
	})
	p.policyLevels[partition] = decision.Level
	metrics.SetPolicyKitOverloadLevel(env.Type, env.Venue, env.Instrument, int(decision.Level))
	stride := decision.DegradeStride()
	enter, recover := activeThresholdsForLevel(decision.Level)
	observability.UpdatePolicyKitOverload(observability.PolicyKitOverloadEntry{
		Stream:        env.Type,
		Venue:         env.Venue,
		OverloadLevel: int(decision.Level),
		Stride:        stride,
		Thresholds: observability.PolicyKitThresholdPair{
			Enter: observability.PolicyKitThreshold{
				QueueRatio:   enter.QueueRatio,
				BacklogRatio: enter.BacklogRatio,
				MapRatio:     enter.MapRatio,
				LatencyMs:    enter.LatencyMs,
			},
			Recover: observability.PolicyKitThreshold{
				QueueRatio:   recover.QueueRatio,
				BacklogRatio: recover.BacklogRatio,
				MapRatio:     recover.MapRatio,
				LatencyMs:    recover.LatencyMs,
			},
		},
	})
	if stride > 1 {
		metrics.IncPolicyKitDegrade(env.Type, fmt.Sprintf("stride_%d", stride))
	}

	applied, keep := p.policyApplier.ApplySingle(decision, env, policykit.ApplyHooks{})
	metrics.ObservePolicyKitLatencyMilliseconds(env.Type, float64(time.Since(started))/float64(time.Millisecond))
	if !keep {
		metrics.IncPolicyKitDrop(env.Type, "policy_drop")
		return env, true
	}
	return applied, false
}

func activeThresholdsForLevel(level policykit.Level) (policykit.Threshold, policykit.Threshold) {
	cfg := policykit.DefaultThresholdConfig()
	switch level {
	case policykit.L3:
		return cfg.EnterL3, cfg.RecoverL3
	case policykit.L2:
		return cfg.EnterL2, cfg.RecoverL2
	default:
		return cfg.EnterL1, cfg.RecoverL1
	}
}

// handleBookDelta decodes a BookDeltaV1 payload and calls UpdateOrderBook.
func (p *ProcessorSubsystemActor) handleBookDelta(env envelope.Envelope) *problem.Problem {
	if p.cfg.UpdateBook == nil {
		p.logger.Warn("aggruntime: no UpdateBook use case configured — dropping bookdelta")
		return problem.New(problem.ValidationFailed, "aggregation UpdateBook use case is not configured")
	}

	decoded, prob := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if prob != nil {
		p.logger.Warn("aggruntime: failed to decode bookdelta payload",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
			"code", prob.Code,
			"err", prob.Message,
		)
		return problem.WithDetail(prob, "reason_code", reasonCodeDecodeFailed)
	}
	delta, ok := decoded.(mddomain.BookDeltaV1)
	if !ok {
		p.logger.Warn("aggruntime: decoded bookdelta payload has unexpected type",
			"decoded_type", fmt.Sprintf("%T", decoded),
		)
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "decoded bookdelta payload type mismatch: got %T", decoded),
				"reason_code", reasonCodeValidationFailed,
			),
			"event_type", env.Type,
		)
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
		return prob
	}

	resp := res.Value()
	p.logger.Debug("aggruntime: order book updated",
		"venue", env.Venue,
		"instrument", env.Instrument,
		"seq", resp.Seq,
		"spread", resp.Spread,
	)
	return nil
}

func (p *ProcessorSubsystemActor) handleTrade(env envelope.Envelope) *problem.Problem {
	if p.cfg.JoinTrades == nil {
		return problem.New(problem.ValidationFailed, "insights JoinTrades use case is not configured")
	}
	if p.cfg.PublishEnvelope == nil {
		return problem.New(problem.ValidationFailed, "insights PublishEnvelope is not configured")
	}

	decoded, prob := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if prob != nil {
		p.logger.Warn("aggruntime: failed to decode trade payload",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
			"code", prob.Code,
			"err", prob.Message,
		)
		return problem.WithDetail(prob, "reason_code", reasonCodeDecodeFailed)
	}
	trade, ok := decoded.(mddomain.TradeTickV1)
	if !ok {
		p.logger.Warn("aggruntime: decoded trade payload has unexpected type",
			"decoded_type", fmt.Sprintf("%T", decoded),
		)
		return problem.WithDetail(
			problem.WithDetail(
				problem.Newf(problem.ValidationFailed, "decoded trade payload type mismatch: got %T", decoded),
				"reason_code", reasonCodeValidationFailed,
			),
			"event_type", env.Type,
		)
	}

	req := insightsapp.JoinCrossVenueTradesRequest{
		Venue:          env.Venue,
		Instrument:     env.Instrument,
		MarketType:     envelopeMarketType(env),
		Price:          trade.Price,
		Size:           trade.Size,
		Side:           trade.Side,
		TradeID:        trade.TradeID,
		TsExchange:     env.TsExchange,
		TsIngest:       env.TsIngest,
		Seq:            env.Seq,
		IdempotencyKey: env.IdempotencyKey,
	}

	res := p.cfg.JoinTrades.Execute(context.Background(), req)
	if res.IsFail() {
		joinProb := res.Problem()
		p.logger.Warn("aggruntime: JoinCrossVenueTrades failed",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
			"code", joinProb.Code,
			"retryable", joinProb.Retryable,
		)
		return joinProb
	}
	if !res.Value().Emitted {
		return nil
	}

	outEnv, prob := buildSnapshotEnvelope(env, res.Value().Snapshot, p.cfg.SnapshotSubjectPrefix)
	if prob != nil {
		return prob
	}
	if prob := p.cfg.PublishEnvelope.Publish(context.Background(), outEnv); prob != nil {
		p.logger.Warn("aggruntime: publish cross-venue snapshot failed",
			"instrument", outEnv.Instrument,
			"seq", outEnv.Seq,
			"code", prob.Code,
			"retryable", prob.Retryable,
		)
		return prob
	}
	p.logger.Debug("aggruntime: cross-venue snapshot published",
		"instrument", outEnv.Instrument,
		"market_type", outEnv.Meta[metaKeyMarketType],
		"watermark_ts_ingest", outEnv.TsIngest,
		"venues", len(res.Value().Snapshot.Venues),
	)

	if !res.Value().SignalEmitted {
		return nil
	}
	signalEnv, prob := buildSpreadSignalEnvelope(env, res.Value().SpreadSignal)
	if prob != nil {
		return prob
	}
	if prob := p.cfg.PublishEnvelope.Publish(context.Background(), signalEnv); prob != nil {
		p.logger.Warn("aggruntime: publish cross-venue spread signal failed",
			"instrument", signalEnv.Instrument,
			"seq", signalEnv.Seq,
			"code", prob.Code,
			"retryable", prob.Retryable,
		)
		return prob
	}
	p.logger.Debug("aggruntime: cross-venue spread signal published",
		"instrument", signalEnv.Instrument,
		"market_type", signalEnv.Meta[metaKeyMarketType],
		"watermark_ts_ingest", signalEnv.TsIngest,
		"spread_bps", res.Value().SpreadSignal.SpreadBps,
	)
	return nil
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

func envelopeMarketType(env envelope.Envelope) string {
	if len(env.Meta) == 0 {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(env.Meta[metaKeyMarketType]))
}

func buildSnapshotEnvelope(
	trigger envelope.Envelope,
	snapshot insightsdomain.CrossVenueTradeSnapshotV1,
	subjectPrefix string,
) (envelope.Envelope, *problem.Problem) {
	payload, p := codec.EncodePayload(
		insightsdomain.CrossVenueTradeSnapshotType,
		insightsdomain.CrossVenueTradeSnapshotVersion,
		snapshotDefaultContentType,
		snapshot,
	)
	if p != nil {
		return envelope.Envelope{}, p
	}

	meta := make(map[string]string, 2)
	if snapshot.MarketType != "" {
		meta[metaKeyMarketType] = snapshot.MarketType
	}
	if prefix := strings.TrimSpace(subjectPrefix); prefix != "" {
		meta[metaKeySubjectPrefix] = prefix
	}
	if len(meta) == 0 {
		meta = nil
	}

	out := envelope.Envelope{
		Type:        insightsdomain.CrossVenueTradeSnapshotType,
		Version:     insightsdomain.CrossVenueTradeSnapshotVersion,
		Venue:       insightsdomain.CrossVenueSnapshotVenue,
		Instrument:  snapshot.Instrument,
		TsExchange:  trigger.TsExchange,
		TsIngest:    snapshot.WatermarkTsIngest,
		Seq:         trigger.Seq,
		ContentType: snapshotDefaultContentType,
		Meta:        meta,
		Payload:     payload,
		IdempotencyKey: sharedhash.HashFields(
			insightsdomain.CrossVenueTradeSnapshotType,
			fmt.Sprintf("%d", insightsdomain.CrossVenueTradeSnapshotVersion),
			strings.ToUpper(strings.TrimSpace(snapshot.Instrument)),
			strings.ToUpper(strings.TrimSpace(snapshot.MarketType)),
			strings.TrimSpace(trigger.IdempotencyKey),
		),
	}
	if p := out.Validate(); p != nil {
		return envelope.Envelope{}, p
	}
	return out, nil
}

func buildSpreadSignalEnvelope(
	trigger envelope.Envelope,
	signal insightsdomain.CrossVenueSpreadSignalV1,
) (envelope.Envelope, *problem.Problem) {
	payload, p := codec.EncodePayload(
		insightsdomain.CrossVenueSpreadSignalType,
		insightsdomain.CrossVenueSpreadSignalVersion,
		snapshotDefaultContentType,
		signal,
	)
	if p != nil {
		return envelope.Envelope{}, p
	}

	meta := make(map[string]string, 1)
	if signal.MarketType != "" {
		meta[metaKeyMarketType] = signal.MarketType
	}
	if len(meta) == 0 {
		meta = nil
	}

	out := envelope.Envelope{
		Type:        insightsdomain.CrossVenueSpreadSignalType,
		Version:     insightsdomain.CrossVenueSpreadSignalVersion,
		Venue:       insightsdomain.CrossVenueSnapshotVenue,
		Instrument:  signal.Instrument,
		TsExchange:  trigger.TsExchange,
		TsIngest:    signal.WatermarkTsIngest,
		Seq:         trigger.Seq,
		ContentType: snapshotDefaultContentType,
		Meta:        meta,
		Payload:     payload,
		IdempotencyKey: sharedhash.HashFields(
			insightsdomain.CrossVenueSpreadSignalType,
			fmt.Sprintf("%d", insightsdomain.CrossVenueSpreadSignalVersion),
			strings.ToUpper(strings.TrimSpace(signal.Instrument)),
			strings.ToUpper(strings.TrimSpace(signal.MarketType)),
			strings.TrimSpace(trigger.IdempotencyKey),
		),
	}
	if p := out.Validate(); p != nil {
		return envelope.Envelope{}, p
	}
	return out, nil
}

func unhandledTypeProblem(eventType string) *problem.Problem {
	return problem.WithDetail(
		problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "unhandled envelope type %q", eventType),
			"reason_code", reasonCodeUnknownEventType,
		),
		"type", eventType,
	)
}

func unsupportedVersionProblem(eventType string, version int) *problem.Problem {
	return problem.WithDetail(
		problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "unsupported envelope version type=%q version=%d", eventType, version),
			"reason_code", reasonCodeUnknownEventVersion,
		),
		"type", eventType,
	)
}

func processorStatus(prob *problem.Problem) string {
	if prob == nil {
		return "ok"
	}
	return "failed"
}

// recordHeartbeat increments counters and emits an Info-level log every
// heartbeatInterval envelopes so operators can confirm the processor is alive.
func (p *ProcessorSubsystemActor) recordHeartbeat(env envelope.Envelope) {
	if p.hbByType == nil {
		p.hbByType = make(map[string]int64)
	}
	p.hbTotal++
	p.hbByType[env.Type]++
	p.hbLastSubject = env.Type
	p.hbLastStreamSeq = env.Seq

	if p.hbTotal%heartbeatInterval == 0 {
		p.logger.Info("aggruntime: processor heartbeat",
			"total_processed", p.hbTotal,
			"by_event_top", p.hbTopTypes(3),
			"last_subject_seen", p.hbLastSubject,
			"last_stream_seq", p.hbLastStreamSeq,
		)
	}
}

// hbTopTypes returns the top-N event types by count as a compact string slice
// (e.g. ["marketdata.bookdelta:4200","marketdata.trade:800"]).
func (p *ProcessorSubsystemActor) hbTopTypes(n int) []string {
	if len(p.hbByType) == 0 {
		return nil
	}
	type kv struct {
		k string
		v int64
	}
	entries := make([]kv, 0, len(p.hbByType))
	for k, v := range p.hbByType {
		entries = append(entries, kv{k, v})
	}
	// Simple selection sort — bounded by number of distinct event types (≤10).
	for i := 0; i < len(entries) && i < n; i++ {
		maxIdx := i
		for j := i + 1; j < len(entries); j++ {
			if entries[j].v > entries[maxIdx].v {
				maxIdx = j
			}
		}
		entries[i], entries[maxIdx] = entries[maxIdx], entries[i]
	}
	limit := n
	if limit > len(entries) {
		limit = len(entries)
	}
	out := make([]string, limit)
	for i := 0; i < limit; i++ {
		out[i] = fmt.Sprintf("%s:%d", entries[i].k, entries[i].v)
	}
	return out
}

func (p *ProcessorSubsystemActor) emitProcessedResult(env envelope.Envelope, prob *problem.Problem) {
	if p.cfg.OnEnvelopeProcessed == nil {
		return
	}
	p.cfg.OnEnvelopeProcessed(EnvelopeProcessResult{
		Envelope: env,
		Problem:  prob,
	})
}
