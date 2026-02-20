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
	"strconv"
	"strings"
	"time"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	insightsapp "github.com/market-raccoon/internal/core/insights/app"
	insightsdomain "github.com/market-raccoon/internal/core/insights/domain"
	insightsports "github.com/market-raccoon/internal/core/insights/ports"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/observability"
	"github.com/market-raccoon/internal/shared/policykit"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	typeBookDelta   = "marketdata.bookdelta"
	typeTrade       = "marketdata.trade"
	typeRaw         = "marketdata.raw"
	typeLiquidation = "marketdata.liquidation"
	typeMarkPrice   = "marketdata.markprice"

	reasonCodeDecodeFailed        = "DECODE_FAILED"
	reasonCodeValidationFailed    = "VALIDATION_FAILED"
	reasonCodeUnknownEventType    = "UNKNOWN_EVENT_TYPE"
	reasonCodeUnknownEventVersion = "UNKNOWN_EVENT_VERSION"

	metaKeyMarketType        = "instrument_market_type"
	metaKeySubjectPrefix     = "subject_prefix"
	metaKeyTimeframe         = "timeframe"
	defaultInsightsTimeframe = "1m"
	defaultInsightsTickSize  = 0.5
)

type heartbeatTickMsg struct{}

// busClosedMsg is sent by the consume goroutine when the envelope channel
// is closed.  It signals the actor to report a fatal failure to Guardian.
type busClosedMsg struct{}

// EnvelopeProcessResult reports the processing outcome for one envelope.
type EnvelopeProcessResult struct {
	Envelope envelope.Envelope
	Problem  *problem.Problem
}

// EventPublisher publishes a canonical envelope to the configured bus adapter.
// Defined here (consumer-side) rather than in core/aggregation/ports because
// publishing is an actor-layer concern — core usecases return domain events,
// the actor decides how/where to publish them.
type EventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

// HeatmapSnapshotStore persists heatmap snapshots into hot storage.
type HeatmapSnapshotStore interface {
	Save(ctx context.Context, artifact insightsdomain.HeatmapArtifactV1, sourceIdempotencyKey string) *problem.Problem
}

// VolumeProfileStore persists VPVR bucket upserts into hot storage.
type VolumeProfileStore interface {
	UpsertVolumeProfileBucket(ctx context.Context, upsert insightsports.VolumeProfileBucketUpsert) *problem.Problem
}

// ProcessorConfig configures the ProcessorSubsystemActor.
type ProcessorConfig struct {
	// Logger is used for structured logging.  Defaults to slog.Default().
	Logger *slog.Logger

	// EnvelopeCh is the source of envelopes to process.  Typically obtained
	// via InMemoryBus.Subscribe().  The actor owns this channel for its
	// lifetime; it must not be shared with other actors.
	EnvelopeCh <-chan envelope.Envelope

	// Service is the aggregation BC facade.
	// Required when routing BookDelta envelopes.
	Service *aggapp.AggregationService
	// CandleEnabled explicitly toggles candle route handling.
	// Nil keeps backward-compatible default (enabled when candle use case exists).
	CandleEnabled *bool
	// StatsEnabled explicitly toggles stats route handling.
	// Nil keeps backward-compatible default (enabled when stats use case exists).
	StatsEnabled *bool

	// JoinTrades is the optional insights use case for cross-venue trade joins.
	JoinTrades *insightsapp.JoinCrossVenueTrades
	// Insights is the optional insights facade for in-memory builders and snapshot queries.
	Insights *insightsapp.InsightsService

	// PublishEnvelope is required when JoinTrades is enabled.
	PublishEnvelope EventPublisher
	// HeatmapStore persists heatmap snapshots into hot storage.
	HeatmapStore HeatmapSnapshotStore
	// VolumeProfileStore persists volume profile bucket upserts into hot storage.
	VolumeProfileStore VolumeProfileStore

	// SnapshotSubjectPrefix optionally overrides publish subject prefix for insight snapshots.
	SnapshotSubjectPrefix string

	// RTPublish controls timer-driven snapshot publishing.
	RTPublish ProcessorRTPublishConfig
	// TickerProducer overrides ticker actor creation (tests only).
	TickerProducer actor.Producer

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

// ProcessorRTPublishConfig controls timer-driven publish cadence.
type ProcessorRTPublishConfig struct {
	OrderbookInterval time.Duration
	HeatmapInterval   time.Duration
	VolumeInterval    time.Duration
}

const (
	// heartbeatInterval emits progress heartbeats every N processed envelopes.
	heartbeatInterval = 1000
	// heartbeatTickInterval drives periodic liveness ticks even when traffic is
	// below heartbeatInterval, avoiding "stuck" perception in low-throughput windows.
	heartbeatTickInterval = 10 * time.Second
	// heartbeatLogInterval bounds timer-driven heartbeat emission frequency.
	heartbeatLogInterval = 20 * time.Second
)

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

	policyApplier    *policykit.Applier
	policyLevels     map[string]policykit.Level
	policyPartitions map[string]string // intern cache: "type|venue|instrument" → same string
	shuttingDown     bool
	tickerPID        *actor.PID

	activeOrderBooks map[aggdomain.BookID]struct{}
	activeHeatmaps   map[insightsapp.HeatmapSnapshotKey]struct{}
	activeVolumes    map[insightsapp.VolumeProfileSnapshotKey]struct{}

	// heartbeat state — pure runtime counters, not persisted.
	hbTotal         int64
	hbByType        map[string]int64
	hbLastSubject   string
	hbLastStreamSeq int64
	hbLastTsIngest  int64
	hbLastEmitAt    time.Time
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
	case actorruntime.Stop:
		p.shuttingDown = true
		p.stopTickerChild(c)
	case actor.Stopped:
		p.onStopped()
	case envelope.Envelope:
		res := p.handleEnvelope(c, msg)
		metrics.IncProcessorProcessed(msg.Type, processorStatus(res))
		p.recordHeartbeat(msg)
		p.emitProcessedResult(msg, res)
	case SnapshotTick:
		p.handleSnapshotTick(msg)
	case heartbeatTickMsg:
		p.emitHeartbeat(false, "timer")
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
	if p.activeOrderBooks == nil {
		p.activeOrderBooks = make(map[aggdomain.BookID]struct{})
	}
	if p.activeHeatmaps == nil {
		p.activeHeatmaps = make(map[insightsapp.HeatmapSnapshotKey]struct{})
	}
	if p.activeVolumes == nil {
		p.activeVolumes = make(map[insightsapp.VolumeProfileSnapshotKey]struct{})
	}
	if p.cfg.PolicyKitEngine != nil {
		if p.policyApplier == nil {
			p.policyApplier = policykit.NewApplier(p.cfg.PolicyKitResolver)
		}
		if p.policyLevels == nil {
			p.policyLevels = make(map[string]policykit.Level)
		}
		if p.policyPartitions == nil {
			p.policyPartitions = make(map[string]string)
		}
	}
}

func (p *ProcessorSubsystemActor) onStarted(c *actor.Context) {
	p.selfPID = c.PID()
	p.engine = c.Engine()
	p.shuttingDown = false

	ctx, cancel := context.WithCancel(context.Background())
	p.stopCancel = cancel

	p.logger.Info("aggruntime: processor started")

	if p.cfg.EnvelopeCh == nil {
		p.logger.Debug("aggruntime: no envelope channel configured — processor idle")
	} else {
		go p.consumeLoop(ctx)
	}
	go p.heartbeatLoop(ctx)
	p.spawnTickerChild(c)
}

func (p *ProcessorSubsystemActor) onStopped() {
	p.shuttingDown = true
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

func (p *ProcessorSubsystemActor) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(heartbeatTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if p.engine != nil && p.selfPID != nil {
				p.engine.Send(p.selfPID, heartbeatTickMsg{})
			}
		case <-ctx.Done():
			return
		}
	}
}

func (p *ProcessorSubsystemActor) spawnTickerChild(c *actor.Context) {
	if p.cfg.PublishEnvelope == nil {
		return
	}
	if p.cfg.RTPublish.OrderbookInterval <= 0 &&
		p.cfg.RTPublish.HeatmapInterval <= 0 &&
		p.cfg.RTPublish.VolumeInterval <= 0 {
		return
	}

	producer := p.cfg.TickerProducer
	if producer == nil {
		producer = NewTickerPublisherActor(TickerPublisherConfig{
			Logger:            p.logger,
			Target:            c.PID(),
			OrderbookInterval: p.cfg.RTPublish.OrderbookInterval,
			HeatmapInterval:   p.cfg.RTPublish.HeatmapInterval,
			VolumeInterval:    p.cfg.RTPublish.VolumeInterval,
		})
	}
	p.tickerPID = c.SpawnChild(producer, "snapshot-ticker")
}

func (p *ProcessorSubsystemActor) stopTickerChild(c *actor.Context) {
	if p.tickerPID == nil || c == nil || c.Engine() == nil {
		return
	}
	c.Engine().Poison(p.tickerPID)
	p.tickerPID = nil
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
		if p.candleEnabled() && p.cfg.Service != nil && p.cfg.Service.Candle != nil {
			if prob := p.handleTradeForCandle(env); prob != nil {
				if isBenignStreamOrderProblem(prob) {
					p.logger.Debug("aggruntime: BuildCandle ignored stale event",
						"venue", env.Venue,
						"instrument", env.Instrument,
						"market_type", envelopeMarketType(env),
						"seq", env.Seq,
						"code", prob.Code,
						"reason", prob.Message,
					)
				} else {
					p.logger.Warn("aggruntime: BuildCandle failed",
						"venue", env.Venue,
						"instrument", env.Instrument,
						"market_type", envelopeMarketType(env),
						"seq", env.Seq,
						"code", prob.Code,
						"reason", prob.Message,
					)
				}
			}
		}
		if prob := p.handleTradeForRealtimeInsights(env); prob != nil {
			p.logger.Warn("aggruntime: realtime insights trade handling failed",
				"venue", env.Venue,
				"instrument", env.Instrument,
				"seq", env.Seq,
				"code", prob.Code,
			)
		}
		if p.cfg.JoinTrades == nil {
			return nil
		}
		return p.handleTrade(env)
	case typeLiquidation:
		if env.Version != 1 {
			return unsupportedVersionProblem(env.Type, env.Version)
		}
		return p.handleLiquidation(env)
	case typeMarkPrice:
		if env.Version != 1 {
			return unsupportedVersionProblem(env.Type, env.Version)
		}
		return p.handleMarkPrice(env)
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

	// Intern the partition key to avoid per-envelope allocations.
	// The number of unique triples is small (bounded by type×venue×instrument),
	// so the cache stays small while eliminating ~1.8M allocs/min.
	partitionKey := env.Type + "|" + env.Venue + "|" + env.Instrument
	partition, ok := p.policyPartitions[partitionKey]
	if !ok {
		partition = partitionKey
		p.policyPartitions[partitionKey] = partition
	}
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
	if p.cfg.Service == nil || p.cfg.Service.UpdateBook == nil {
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
		Instrument: orderBookInstrumentKey(env),
		Seq:        orderBookSeq(env, delta),
		Bids:       toLevels(delta.Bids),
		Asks:       toLevels(delta.Asks),
		IsSnapshot: delta.IsSnapshot,
	}

	res := p.cfg.Service.UpdateBook.Execute(context.Background(), req)
	if res.IsFail() {
		prob := res.Problem()
		if isBenignBookProblem(prob) {
			p.logger.Debug("aggruntime: UpdateOrderBook ignored stale/inconsistent event",
				"venue", env.Venue,
				"instrument", env.Instrument,
				"seq", req.Seq,
				"code", prob.Code,
			)
			return nil
		}
		p.logger.Warn("aggruntime: UpdateOrderBook failed",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", req.Seq,
			"code", prob.Code,
			"retryable", prob.Retryable,
		)
		return prob
	}

	resp := res.Value()
	p.markOrderBookActive(env.Venue, orderBookInstrumentKey(env))
	p.handleBookDeltaForInsights(env, delta)
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

func (p *ProcessorSubsystemActor) handleTradeForRealtimeInsights(env envelope.Envelope) *problem.Problem {
	if p.cfg.Insights == nil || (p.cfg.Insights.Heatmap == nil && p.cfg.Insights.VolumeProfile == nil) {
		return nil
	}

	decoded, prob := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if prob != nil {
		return problem.WithDetail(prob, "reason_code", reasonCodeDecodeFailed)
	}
	trade, ok := decoded.(mddomain.TradeTickV1)
	if !ok {
		return problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "decoded trade payload type mismatch: got %T", decoded),
			"reason_code", reasonCodeValidationFailed,
		)
	}

	p.handleTradeForInsights(env, trade)
	return nil
}

func (p *ProcessorSubsystemActor) handleBookDeltaForInsights(env envelope.Envelope, delta mddomain.BookDeltaV1) {
	if p.cfg.Insights == nil || p.cfg.Insights.Heatmap == nil {
		return
	}

	timeframe := defaultInsightsTimeframe
	if len(delta.Bids) > 0 {
		_ = p.cfg.Insights.Heatmap.Execute(context.Background(), insightsapp.BuildHeatmapRequest{
			EventType:  env.Type,
			Venue:      env.Venue,
			Instrument: stateInstrumentKey(env),
			Timeframe:  timeframe,
			TickSize:   defaultInsightsTickSize,
			Price:      delta.Bids[0].Price,
			Size:       delta.Bids[0].Size,
			Side:       "buy",
			TsIngest:   env.TsIngest,
			Seq:        env.Seq,
		})
	}
	if len(delta.Asks) > 0 {
		_ = p.cfg.Insights.Heatmap.Execute(context.Background(), insightsapp.BuildHeatmapRequest{
			EventType:  env.Type,
			Venue:      env.Venue,
			Instrument: stateInstrumentKey(env),
			Timeframe:  timeframe,
			TickSize:   defaultInsightsTickSize,
			Price:      delta.Asks[0].Price,
			Size:       delta.Asks[0].Size,
			Side:       "sell",
			TsIngest:   env.TsIngest,
			Seq:        env.Seq,
		})
	}
	p.markHeatmapActive(env.Venue, stateInstrumentKey(env), timeframe)
}

func (p *ProcessorSubsystemActor) handleTradeForInsights(env envelope.Envelope, trade mddomain.TradeTickV1) {
	if p.cfg.Insights == nil {
		return
	}

	timeframe := defaultInsightsTimeframe
	if p.cfg.Insights.Heatmap != nil {
		res := p.cfg.Insights.Heatmap.Execute(context.Background(), insightsapp.BuildHeatmapRequest{
			EventType:  env.Type,
			Venue:      env.Venue,
			Instrument: stateInstrumentKey(env),
			Timeframe:  timeframe,
			TickSize:   defaultInsightsTickSize,
			Price:      trade.Price,
			Size:       trade.Size,
			Side:       trade.Side,
			TsIngest:   env.TsIngest,
			Seq:        env.Seq,
		})
		if res.IsOk() {
			p.markHeatmapActive(env.Venue, stateInstrumentKey(env), timeframe)
		}
	}
	if p.cfg.Insights.VolumeProfile != nil {
		res := p.cfg.Insights.VolumeProfile.Execute(context.Background(), insightsapp.BuildVolumeProfileRequest{
			EventType:  env.Type,
			Venue:      env.Venue,
			Instrument: stateInstrumentKey(env),
			Timeframe:  timeframe,
			TickSize:   defaultInsightsTickSize,
			Price:      trade.Price,
			Size:       trade.Size,
			Side:       trade.Side,
			TsIngest:   env.TsIngest,
			Seq:        env.Seq,
		})
		if res.IsOk() && res.Value().Emitted {
			p.markVolumeActive(env.Venue, stateInstrumentKey(env), timeframe)
		}
	}
}

func (p *ProcessorSubsystemActor) handleTradeForCandle(env envelope.Envelope) *problem.Problem {
	if !p.candleEnabled() {
		return nil
	}
	if p.cfg.Service == nil || p.cfg.Service.Candle == nil {
		return nil
	}

	decoded, prob := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if prob != nil {
		return problem.WithDetail(prob, "reason_code", reasonCodeDecodeFailed)
	}
	trade, ok := decoded.(mddomain.TradeTickV1)
	if !ok {
		return problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "decoded trade payload type mismatch: got %T", decoded),
			"reason_code", reasonCodeValidationFailed,
		)
	}
	req := aggapp.BuildCandleRequest{
		Venue:      env.Venue,
		Instrument: stateInstrumentKey(env),
		Price:      trade.Price,
		Quantity:   trade.Size,
		IsBuy:      strings.EqualFold(trade.Side, "buy"),
		Seq:        env.Seq,
		TsIngest:   env.TsIngest,
	}
	resp, prob := p.cfg.Service.Candle.Execute(context.Background(), req)
	if prob != nil {
		return prob
	}
	if len(resp.Closed) > 0 {
		p.logger.Debug("aggruntime: candles closed",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"closed_count", len(resp.Closed),
			"active_candles", resp.ActiveCandles,
		)
	}
	return nil
}

func (p *ProcessorSubsystemActor) handleLiquidation(env envelope.Envelope) *problem.Problem {
	if !p.statsEnabled() {
		return nil
	}
	if p.cfg.Service == nil || p.cfg.Service.Stats == nil {
		p.logger.Warn("aggruntime: no Stats use case configured — dropping liquidation")
		return nil
	}

	decoded, prob := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if prob != nil {
		return problem.WithDetail(prob, "reason_code", reasonCodeDecodeFailed)
	}
	liq, ok := decoded.(mddomain.LiquidationTickV1)
	if !ok {
		return problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "decoded liquidation type mismatch: got %T", decoded),
			"reason_code", reasonCodeValidationFailed,
		)
	}
	req := aggapp.BuildStatsRequest{
		Venue:           env.Venue,
		Instrument:      stateInstrumentKey(env),
		Kind:            aggapp.StatsInputLiquidation,
		Seq:             env.Seq,
		TsIngest:        env.TsIngest,
		LiquidationSide: liq.Side,
		LiquidationQty:  liq.Size,
	}
	resp, prob := p.cfg.Service.Stats.Execute(context.Background(), req)
	if prob != nil {
		if isBenignStreamOrderProblem(prob) {
			p.logger.Debug("aggruntime: BuildStats ignored stale liquidation",
				"venue", env.Venue,
				"instrument", env.Instrument,
				"market_type", envelopeMarketType(env),
				"seq", env.Seq,
				"code", prob.Code,
				"reason", prob.Message,
			)
			return nil
		}
		return prob
	}
	if len(resp.Closed) > 0 {
		p.logger.Debug("aggruntime: stats windows closed (liquidation)",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"closed_count", len(resp.Closed),
		)
	}
	return nil
}

func (p *ProcessorSubsystemActor) handleMarkPrice(env envelope.Envelope) *problem.Problem {
	if !p.statsEnabled() {
		return nil
	}
	if p.cfg.Service == nil || p.cfg.Service.Stats == nil {
		p.logger.Warn("aggruntime: no Stats use case configured — dropping markprice")
		return nil
	}

	decoded, prob := codec.DecodePayload(env.Type, env.Version, env.ContentType, env.Payload)
	if prob != nil {
		return problem.WithDetail(prob, "reason_code", reasonCodeDecodeFailed)
	}
	mark, ok := decoded.(mddomain.MarkPriceTickV1)
	if !ok {
		return problem.WithDetail(
			problem.Newf(problem.ValidationFailed, "decoded markprice type mismatch: got %T", decoded),
			"reason_code", reasonCodeValidationFailed,
		)
	}
	markReq := aggapp.BuildStatsRequest{
		Venue:      env.Venue,
		Instrument: stateInstrumentKey(env),
		Kind:       aggapp.StatsInputMarkPrice,
		Seq:        env.Seq,
		TsIngest:   env.TsIngest,
		MarkPrice:  mark.MarkPrice,
	}
	resp, prob := p.cfg.Service.Stats.Execute(context.Background(), markReq)
	if prob != nil {
		if isBenignStreamOrderProblem(prob) {
			p.logger.Debug("aggruntime: BuildStats ignored stale markprice",
				"venue", env.Venue,
				"instrument", env.Instrument,
				"market_type", envelopeMarketType(env),
				"seq", env.Seq,
				"code", prob.Code,
				"reason", prob.Message,
			)
			return nil
		}
		return prob
	}
	if mark.FundingRate != 0 {
		fundingReq := aggapp.BuildStatsRequest{
			Venue:       env.Venue,
			Instrument:  stateInstrumentKey(env),
			Kind:        aggapp.StatsInputFundingRate,
			Seq:         env.Seq,
			TsIngest:    env.TsIngest,
			FundingRate: mark.FundingRate,
		}
		resp, prob = p.cfg.Service.Stats.Execute(context.Background(), fundingReq)
		if prob != nil {
			if isBenignStreamOrderProblem(prob) {
				p.logger.Debug("aggruntime: BuildStats ignored stale funding",
					"venue", env.Venue,
					"instrument", env.Instrument,
					"market_type", envelopeMarketType(env),
					"seq", env.Seq,
					"code", prob.Code,
					"reason", prob.Message,
				)
				return nil
			}
			return prob
		}
	}
	if len(resp.Closed) > 0 {
		p.logger.Debug("aggruntime: stats windows closed (markprice)",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"closed_count", len(resp.Closed),
		)
	}
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

func (p *ProcessorSubsystemActor) candleEnabled() bool {
	if p.cfg.CandleEnabled == nil {
		return true
	}
	return *p.cfg.CandleEnabled
}

func (p *ProcessorSubsystemActor) statsEnabled() bool {
	if p.cfg.StatsEnabled == nil {
		return true
	}
	return *p.cfg.StatsEnabled
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

func orderBookInstrumentKey(env envelope.Envelope) string {
	return stateInstrumentKey(env)
}

func stateInstrumentKey(env envelope.Envelope) string {
	marketType := envelopeMarketType(env)
	if marketType == "" {
		return env.Instrument
	}
	return env.Instrument + ":" + marketType
}

func orderBookSeq(env envelope.Envelope, delta mddomain.BookDeltaV1) int64 {
	if delta.FinalID > 0 {
		return delta.FinalID
	}
	return env.Seq
}

func isBenignStreamOrderProblem(prob *problem.Problem) bool {
	if prob == nil {
		return false
	}
	return prob.Code == problem.OutOfOrder
}

func isBenignBookProblem(prob *problem.Problem) bool {
	if prob == nil {
		return false
	}
	return prob.Code == problem.OutOfOrder || prob.Code == problem.IntegrityViolation
}

func (p *ProcessorSubsystemActor) markOrderBookActive(venue, instrument string) {
	p.activeOrderBooks[aggdomain.BookID{
		Venue:      venue,
		Instrument: instrument,
	}] = struct{}{}
}

func (p *ProcessorSubsystemActor) markHeatmapActive(venue, instrument, timeframe string) {
	p.activeHeatmaps[insightsapp.HeatmapSnapshotKey{
		Venue:      venue,
		Instrument: instrument,
		Timeframe:  timeframe,
	}] = struct{}{}
}

func (p *ProcessorSubsystemActor) markVolumeActive(venue, instrument, timeframe string) {
	p.activeVolumes[insightsapp.VolumeProfileSnapshotKey{
		Venue:      venue,
		Instrument: instrument,
		Timeframe:  timeframe,
	}] = struct{}{}
}

func (p *ProcessorSubsystemActor) handleSnapshotTick(msg SnapshotTick) {
	if p.shuttingDown || p.cfg.PublishEnvelope == nil {
		return
	}

	switch msg.Kind {
	case SnapshotTickOrderBook:
		p.publishOrderBookSnapshots()
	case SnapshotTickHeatmap:
		p.publishHeatmapSnapshots()
	case SnapshotTickVolume:
		p.publishVolumeSnapshots()
	}
}

func (p *ProcessorSubsystemActor) publishOrderBookSnapshots() {
	if p.cfg.Service == nil {
		return
	}
	for key := range p.activeOrderBooks {
		res := p.cfg.Service.SnapshotOrderBook(context.Background(), key)
		if res.IsFail() {
			continue
		}
		env, prob := buildOrderbookSnapshotEnvelope(res.Value(), time.Now().UnixMilli())
		if prob != nil {
			continue
		}
		if prob := p.cfg.PublishEnvelope.Publish(context.Background(), env); prob != nil {
			p.logger.Warn("aggruntime: publish orderbook snapshot tick failed",
				"venue", key.Venue,
				"instrument", key.Instrument,
				"code", prob.Code,
			)
		}
	}
}

func (p *ProcessorSubsystemActor) publishHeatmapSnapshots() {
	if p.cfg.Insights == nil {
		return
	}
	for key := range p.activeHeatmaps {
		res := p.cfg.Insights.SnapshotHeatmap(context.Background(), key)
		if res.IsFail() {
			continue
		}
		env, prob := buildHeatmapSnapshotEnvelope(res.Value(), time.Now().UnixMilli())
		if prob != nil {
			continue
		}
		if p.cfg.HeatmapStore != nil {
			if prob := p.cfg.HeatmapStore.Save(context.Background(), res.Value(), env.IdempotencyKey); prob != nil {
				p.logger.Warn("aggruntime: persist heatmap snapshot failed",
					"venue", key.Venue,
					"instrument", key.Instrument,
					"timeframe", key.Timeframe,
					"code", prob.Code,
				)
			}
		}
		if prob := p.cfg.PublishEnvelope.Publish(context.Background(), env); prob != nil {
			p.logger.Warn("aggruntime: publish heatmap snapshot tick failed",
				"venue", key.Venue,
				"instrument", key.Instrument,
				"timeframe", key.Timeframe,
				"code", prob.Code,
			)
		}
	}
}

func (p *ProcessorSubsystemActor) publishVolumeSnapshots() {
	if p.cfg.Insights == nil {
		return
	}
	for key := range p.activeVolumes {
		res := p.cfg.Insights.SnapshotVolumeProfile(context.Background(), key)
		if res.IsFail() {
			continue
		}
		env, prob := buildVolumeSnapshotEnvelope(res.Value(), time.Now().UnixMilli())
		if prob != nil {
			continue
		}
		if p.cfg.VolumeProfileStore != nil {
			for _, bucket := range res.Value().Buckets {
				upsert := insightsports.VolumeProfileBucketUpsert{
					Venue:         res.Value().Venue,
					Instrument:    res.Value().Instrument,
					Timeframe:     res.Value().Timeframe,
					WindowStartTs: res.Value().WindowStartTs,
					BucketLow:     bucket.PriceLow,
					BucketHigh:    bucket.PriceHigh,
					BuyVolume:     bucket.BuyVolume,
					SellVolume:    bucket.SellVolume,
					TotalVolume:   bucket.TotalVolume,
					SeqMin:        bucket.SeqMin,
					SeqMax:        bucket.SeqMax,
				}
				if prob := p.cfg.VolumeProfileStore.UpsertVolumeProfileBucket(context.Background(), upsert); prob != nil {
					p.logger.Warn("aggruntime: persist volume snapshot bucket failed",
						"venue", key.Venue,
						"instrument", key.Instrument,
						"timeframe", key.Timeframe,
						"code", prob.Code,
					)
					break
				}
			}
		}
		if prob := p.cfg.PublishEnvelope.Publish(context.Background(), env); prob != nil {
			p.logger.Warn("aggruntime: publish volume snapshot tick failed",
				"venue", key.Venue,
				"instrument", key.Instrument,
				"timeframe", key.Timeframe,
				"code", prob.Code,
			)
		}
	}
}

// resolveContentType selects proto or JSON based on rollout flags for the given event type.
func resolveContentType(eventType string) string {
	if contracts.ProtoRolloutEnabledForEventType(eventType) {
		return envelope.ContentTypeProto
	}
	return envelope.ContentTypeJSON
}

func buildOrderbookSnapshotEnvelope(snapshot aggdomain.SnapshotProduced, nowMs int64) (envelope.Envelope, *problem.Problem) {
	payload, p := codec.Marshal(snapshot)
	if p != nil {
		return envelope.Envelope{}, p
	}
	out := envelope.Envelope{
		Type:        "aggregation.snapshot",
		Version:     1,
		Venue:       snapshot.BookID.Venue,
		Instrument:  snapshot.BookID.Instrument,
		TsIngest:    nowMs,
		Seq:         snapshot.Seq,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
		IdempotencyKey: sharedhash.HashFieldsFast(
			"aggregation.snapshot",
			snapshot.BookID.Venue,
			snapshot.BookID.Instrument,
			strconv.FormatInt(snapshot.Seq, 10),
			strconv.FormatInt(nowMs, 10),
		),
	}
	if p := out.Validate(); p != nil {
		return envelope.Envelope{}, p
	}
	return out, nil
}

func buildHeatmapSnapshotEnvelope(snapshot insightsdomain.HeatmapArtifactV1, nowMs int64) (envelope.Envelope, *problem.Problem) {
	ct := resolveContentType(insightsdomain.HeatmapSnapshotType)
	payload, p := codec.EncodePayload(
		insightsdomain.HeatmapSnapshotType,
		insightsdomain.HeatmapSnapshotVersion,
		ct,
		snapshot,
	)
	if p != nil {
		return envelope.Envelope{}, p
	}
	out := envelope.Envelope{
		Type:        insightsdomain.HeatmapSnapshotType,
		Version:     insightsdomain.HeatmapSnapshotVersion,
		Venue:       snapshot.Venue,
		Instrument:  snapshot.Instrument,
		TsIngest:    nowMs,
		Seq:         heatmapSeq(snapshot),
		ContentType: ct,
		Meta:        map[string]string{metaKeyTimeframe: snapshot.Timeframe},
		Payload:     payload,
		IdempotencyKey: sharedhash.HashFieldsFast(
			insightsdomain.HeatmapSnapshotType,
			snapshot.Venue,
			snapshot.Instrument,
			snapshot.Timeframe,
			strconv.FormatInt(snapshot.WindowStartTs, 10),
			strconv.FormatInt(nowMs, 10),
		),
	}
	if p := out.Validate(); p != nil {
		return envelope.Envelope{}, p
	}
	return out, nil
}

func buildVolumeSnapshotEnvelope(snapshot insightsdomain.VolumeProfileSnapshotV1, nowMs int64) (envelope.Envelope, *problem.Problem) {
	ct := resolveContentType(insightsdomain.VolumeProfileSnapshotType)
	payload, p := codec.EncodePayload(
		insightsdomain.VolumeProfileSnapshotType,
		insightsdomain.VolumeProfileSnapshotVersion,
		ct,
		snapshot,
	)
	if p != nil {
		return envelope.Envelope{}, p
	}
	out := envelope.Envelope{
		Type:        insightsdomain.VolumeProfileSnapshotType,
		Version:     insightsdomain.VolumeProfileSnapshotVersion,
		Venue:       snapshot.Venue,
		Instrument:  snapshot.Instrument,
		TsIngest:    nowMs,
		Seq:         volumeSeq(snapshot),
		ContentType: ct,
		Meta:        map[string]string{metaKeyTimeframe: snapshot.Timeframe},
		Payload:     payload,
		IdempotencyKey: sharedhash.HashFieldsFast(
			insightsdomain.VolumeProfileSnapshotType,
			snapshot.Venue,
			snapshot.Instrument,
			snapshot.Timeframe,
			strconv.FormatInt(snapshot.WindowStartTs, 10),
			strconv.FormatInt(nowMs, 10),
		),
	}
	if p := out.Validate(); p != nil {
		return envelope.Envelope{}, p
	}
	return out, nil
}

func heatmapSeq(snapshot insightsdomain.HeatmapArtifactV1) int64 {
	var seq int64
	for _, cell := range snapshot.Cells {
		if cell.SeqMax > seq {
			seq = cell.SeqMax
		}
	}
	return seq
}

func volumeSeq(snapshot insightsdomain.VolumeProfileSnapshotV1) int64 {
	var seq int64
	for _, bucket := range snapshot.Buckets {
		if bucket.SeqMax > seq {
			seq = bucket.SeqMax
		}
	}
	return seq
}

func buildSnapshotEnvelope(
	trigger envelope.Envelope,
	snapshot insightsdomain.CrossVenueTradeSnapshotV1,
	subjectPrefix string,
) (envelope.Envelope, *problem.Problem) {
	ct := resolveContentType(insightsdomain.CrossVenueTradeSnapshotType)
	payload, p := codec.EncodePayload(
		insightsdomain.CrossVenueTradeSnapshotType,
		insightsdomain.CrossVenueTradeSnapshotVersion,
		ct,
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
		ContentType: ct,
		Meta:        meta,
		Payload:     payload,
		IdempotencyKey: sharedhash.HashFieldsFast(
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
	ct := resolveContentType(insightsdomain.CrossVenueSpreadSignalType)
	payload, p := codec.EncodePayload(
		insightsdomain.CrossVenueSpreadSignalType,
		insightsdomain.CrossVenueSpreadSignalVersion,
		ct,
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
		ContentType: ct,
		Meta:        meta,
		Payload:     payload,
		IdempotencyKey: sharedhash.HashFieldsFast(
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
	p.hbLastTsIngest = env.TsIngest

	p.emitHeartbeat(p.hbTotal%heartbeatInterval == 0, "progress")
}

func (p *ProcessorSubsystemActor) emitHeartbeat(force bool, trigger string) {
	now := time.Now()
	if !shouldEmitHeartbeat(now, p.hbLastEmitAt, force, heartbeatLogInterval) {
		return
	}
	p.hbLastEmitAt = now
	p.logger.Info("aggruntime: processor heartbeat",
		"trigger", trigger,
		"total_processed", p.hbTotal,
		"by_event_top", p.hbTopTypes(3),
		"last_subject_seen", p.hbLastSubject,
		"last_stream_seq", p.hbLastStreamSeq,
		"last_ts_ingest", p.hbLastTsIngest,
	)
}

func shouldEmitHeartbeat(now, last time.Time, force bool, interval time.Duration) bool {
	if force {
		return true
	}
	if interval <= 0 {
		return true
	}
	if last.IsZero() {
		return true
	}
	return now.Sub(last) >= interval
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
