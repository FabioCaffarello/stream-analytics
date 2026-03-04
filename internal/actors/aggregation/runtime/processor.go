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
	"sort"
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
	"github.com/market-raccoon/internal/shared/naming"
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
	typeXVenueBook  = "aggregation.crossvenue_book"

	xVenueBookVersion = 1
	xVenueBookVenue   = "crossvenue"

	reasonCodeDecodeFailed        = "DECODE_FAILED"
	reasonCodeValidationFailed    = "VALIDATION_FAILED"
	reasonCodeUnknownEventType    = "UNKNOWN_EVENT_TYPE"
	reasonCodeUnknownEventVersion = "UNKNOWN_EVENT_VERSION"

	metaKeyMarketType                = "instrument_market_type"
	metaKeySubjectPrefix             = "subject_prefix"
	metaKeyTimeframe                 = "timeframe"
	defaultInsightsTimeframe         = "1m"
	defaultInsightsTickSize          = 0.5
	insightsHeatmapBookLevelsPerSide = 8

	defaultXVenueStaleThreshold = 30 * time.Second
	defaultXVenueMaxInstruments = 2048
	defaultXVenueMaxVenues      = 6
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
	// CrossVenueMerger merges venue top-of-book snapshots into synthetic global view.
	CrossVenueMerger aggdomain.CrossVenueBookMerger
	// CrossVenue configures synthetic cross-venue book snapshots derived from order book updates.
	CrossVenue ProcessorCrossVenueConfig
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
	// CatchUpSkipBookDeltaSkew, when > 0, skips stale marketdata.bookdelta
	// envelopes while the processor is catching up (based on ingest watermark skew).
	// This is intended for local/dev throughput relief and is disabled by default.
	CatchUpSkipBookDeltaSkew time.Duration
	// CatchUpSkipTradeSkew, when > 0, skips stale marketdata.trade envelopes
	// while the processor is catching up (based on ingest watermark skew).
	// This is intended for local/dev throughput relief and is disabled by default.
	CatchUpSkipTradeSkew time.Duration
	// CatchUpSkipStatsSkew, when > 0, skips stale marketdata.liquidation and
	// marketdata.markprice envelopes while the processor is catching up
	// (based on ingest watermark skew). This is intended for local/dev throughput
	// relief and is disabled by default.
	CatchUpSkipStatsSkew time.Duration
	// InsightsTimeframes lists TFs for heatmap/VPVR generation. Default: ["1m"].
	InsightsTimeframes []string
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

	// Now is an optional clock source for runtime decisions/log throttling.
	// When nil, time.Now is used.
	Now func() time.Time
}

// ProcessorCrossVenueConfig controls deterministic cross-venue snapshot behavior.
type ProcessorCrossVenueConfig struct {
	// Enabled toggles synthetic cross-venue snapshot publishing on book updates.
	Enabled bool
	// StaleThreshold excludes venue books whose last ingest timestamp is older than threshold.
	StaleThreshold time.Duration
	// MaxInstruments bounds active instrument partitions kept in memory.
	MaxInstruments int
	// MaxVenues bounds venue top-of-book entries per instrument.
	MaxVenues int
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
	// When processor ingest is far behind wall clock, periodic snapshots become stale
	// and expensive; defer them temporarily so the actor can catch up on envelopes.
	snapshotTickDeferSkewThreshold = 30 * time.Second
	snapshotTickDeferLogInterval   = 20 * time.Second
	// policyPartitionCacheCap bounds interned "type|venue|instrument" keys used by PolicyKit.
	policyPartitionCacheCap = 4096
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
	policyPartitionQ []string          // deterministic FIFO ring for bounded partition eviction.
	policyPartitionI int
	shuttingDown     bool
	tickerPID        *actor.PID

	activeOrderBooks map[aggdomain.BookID]struct{}
	activeHeatmaps   map[insightsapp.HeatmapSnapshotKey]struct{}
	activeVolumes    map[insightsapp.VolumeProfileSnapshotKey]struct{}

	crossVenueBooks       map[string]map[string]aggdomain.CrossVenueVenueBook
	crossVenueInstrumentQ []string
	crossVenueSeq         map[string]int64

	// heartbeat state — pure runtime counters, not persisted.
	hbTotal                       int64
	hbByType                      map[string]int64
	hbLastSubject                 string
	hbLastStreamSeq               int64
	hbLastTsIngest                int64
	hbLastEmitAt                  time.Time
	snapshotTickDeferLastLogAt    time.Time
	bookDeltaCatchUpSkipLastLogAt time.Time
	tradeCatchUpSkipLastLogAt     time.Time
	liquidationCatchUpSkipLogAt   time.Time
	markPriceCatchUpSkipLogAt     time.Time
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
	if p.crossVenueBooks == nil {
		p.crossVenueBooks = make(map[string]map[string]aggdomain.CrossVenueVenueBook)
	}
	if p.crossVenueSeq == nil {
		p.crossVenueSeq = make(map[string]int64)
	}
	if p.cfg.CrossVenue.StaleThreshold <= 0 {
		p.cfg.CrossVenue.StaleThreshold = defaultXVenueStaleThreshold
	}
	if p.cfg.CrossVenue.MaxInstruments <= 0 {
		p.cfg.CrossVenue.MaxInstruments = defaultXVenueMaxInstruments
	}
	if p.cfg.CrossVenue.MaxVenues <= 0 {
		p.cfg.CrossVenue.MaxVenues = defaultXVenueMaxVenues
	}
	if p.cfg.CrossVenueMerger == nil {
		p.cfg.CrossVenueMerger = aggdomain.DeterministicCrossVenueBookMerger{}
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
		if p.policyPartitionQ == nil {
			p.policyPartitionQ = make([]string, 0, policyPartitionCacheCap)
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
		if p.shouldSkipBookDeltaForCatchUp(env) {
			return nil
		}
		return p.handleBookDelta(env)
	case typeTrade:
		if env.Version != 1 {
			return unsupportedVersionProblem(env.Type, env.Version)
		}
		if p.shouldSkipTradeForCatchUp(env) {
			return nil
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
		} else if !p.candleEnabled() {
			metrics.IncIngestDrop("candle_route_disabled")
		} else {
			metrics.IncIngestDrop("candle_route_unconfigured")
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
		if p.shouldSkipLiquidationForCatchUp(env) {
			return nil
		}
		return p.handleLiquidation(env)
	case typeMarkPrice:
		if env.Version != 1 {
			return unsupportedVersionProblem(env.Type, env.Version)
		}
		if p.shouldSkipMarkPriceForCatchUp(env) {
			return nil
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
	partition := p.internPolicyPartitionWithCap(partitionKey, policyPartitionCacheCap)
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
	metrics.ObservePolicyKitLatencySeconds(env.Type, time.Since(started).Seconds())
	if !keep {
		metrics.IncPolicyKitDrop(env.Type, "policy_drop")
		return env, true
	}
	return applied, false
}

func (p *ProcessorSubsystemActor) internPolicyPartitionWithCap(partitionKey string, maxEntries int) string {
	if maxEntries <= 0 {
		maxEntries = 1
	}
	if p.policyPartitions == nil {
		p.policyPartitions = make(map[string]string, maxEntries)
	}
	if p.policyLevels == nil {
		p.policyLevels = make(map[string]policykit.Level, maxEntries)
	}
	if p.policyPartitionQ == nil {
		p.policyPartitionQ = make([]string, 0, maxEntries)
	}
	if partition, ok := p.policyPartitions[partitionKey]; ok {
		return partition
	}

	if len(p.policyPartitions) >= maxEntries {
		if len(p.policyPartitionQ) == 0 {
			keys := make([]string, 0, len(p.policyPartitions))
			for key := range p.policyPartitions {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			if len(keys) > 0 {
				evictKey := keys[0]
				if evictPartition, ok := p.policyPartitions[evictKey]; ok {
					delete(p.policyPartitions, evictKey)
					delete(p.policyLevels, evictPartition)
				}
			}
		} else {
			if p.policyPartitionI < 0 || p.policyPartitionI >= len(p.policyPartitionQ) {
				p.policyPartitionI = 0
			}
			evictKey := p.policyPartitionQ[p.policyPartitionI]
			if evictPartition, ok := p.policyPartitions[evictKey]; ok {
				delete(p.policyPartitions, evictKey)
				delete(p.policyLevels, evictPartition)
			}
			p.policyPartitionQ[p.policyPartitionI] = partitionKey
			p.policyPartitionI = (p.policyPartitionI + 1) % maxEntries
		}
	} else {
		p.policyPartitionQ = append(p.policyPartitionQ, partitionKey)
		if len(p.policyPartitionQ) == maxEntries {
			p.policyPartitionI = 0
		}
	}

	p.policyPartitions[partitionKey] = partitionKey
	return partitionKey
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
		metrics.IncIngestDrop("orderbook_route_unconfigured")
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
			"message", prob.Message,
			"cause", prob.Cause,
		)
		return prob
	}

	resp := res.Value()
	p.markOrderBookActive(env.Venue, orderBookInstrumentKey(env))
	p.handleBookDeltaForInsights(env, delta)
	if prob := p.handleBookDeltaForCrossVenue(env, req.Instrument); prob != nil {
		p.logger.Warn("aggruntime: cross-venue book merge failed",
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", req.Seq,
			"code", prob.Code,
			"message", prob.Message,
		)
	}
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

	instrument := stateInstrumentKey(env)
	for _, timeframe := range p.insightsTimeframes() {
		bidDepth := min(len(delta.Bids), insightsHeatmapBookLevelsPerSide)
		for i := 0; i < bidDepth; i++ {
			bid := delta.Bids[i]
			if bid.Price <= 0 || bid.Size <= 0 {
				continue
			}
			_ = p.cfg.Insights.Heatmap.Execute(context.Background(), insightsapp.BuildHeatmapRequest{
				EventType:  env.Type,
				Venue:      env.Venue,
				Instrument: instrument,
				Timeframe:  timeframe,
				TickSize:   defaultInsightsTickSize,
				Price:      bid.Price,
				Size:       bid.Size,
				Side:       "buy",
				TsIngest:   env.TsIngest,
				Seq:        env.Seq,
			})
		}
		askDepth := min(len(delta.Asks), insightsHeatmapBookLevelsPerSide)
		for i := 0; i < askDepth; i++ {
			ask := delta.Asks[i]
			if ask.Price <= 0 || ask.Size <= 0 {
				continue
			}
			_ = p.cfg.Insights.Heatmap.Execute(context.Background(), insightsapp.BuildHeatmapRequest{
				EventType:  env.Type,
				Venue:      env.Venue,
				Instrument: instrument,
				Timeframe:  timeframe,
				TickSize:   defaultInsightsTickSize,
				Price:      ask.Price,
				Size:       ask.Size,
				Side:       "sell",
				TsIngest:   env.TsIngest,
				Seq:        env.Seq,
			})
		}
		p.markHeatmapActive(env.Venue, instrument, timeframe)
	}
}

func (p *ProcessorSubsystemActor) handleBookDeltaForCrossVenue(env envelope.Envelope, instrumentKey string) *problem.Problem {
	if !p.cfg.CrossVenue.Enabled || p.cfg.PublishEnvelope == nil {
		return nil
	}
	if p.cfg.Service == nil || p.cfg.Service.UpdateBook == nil || p.cfg.CrossVenueMerger == nil {
		return nil
	}

	nowMs := env.TsIngest
	if nowMs <= 0 {
		return problem.New(problem.ValidationFailed, "cross-venue merge requires ts_ingest > 0")
	}

	snapshot, prob := p.cfg.Service.UpdateBook.Snapshot(env.Venue, instrumentKey)
	if prob != nil {
		return prob
	}
	venue := strings.ToUpper(strings.TrimSpace(env.Venue))
	if venue == "" {
		return nil
	}

	venueBook := aggdomain.CrossVenueVenueBook{
		Venue:    venue,
		TsIngest: nowMs,
		Seq:      snapshot.Seq,
	}
	if len(snapshot.Bids) > 0 {
		bid := snapshot.Bids[0]
		venueBook.BestBid = &bid
	}
	if len(snapshot.Asks) > 0 {
		ask := snapshot.Asks[0]
		venueBook.BestAsk = &ask
	}

	p.upsertCrossVenueBook(instrumentKey, venueBook)
	books := p.collectCrossVenueBooks(instrumentKey)

	mergeStartedAt := time.Now()
	merged, mergeProb := p.cfg.CrossVenueMerger.Merge(
		naming.StripMarketType(instrumentKey),
		nowMs,
		books,
		p.cfg.CrossVenue.StaleThreshold.Milliseconds(),
	)
	metrics.ObserveMRXVenueMergeDuration(instrumentKey, time.Since(mergeStartedAt))
	if mergeProb != nil {
		return mergeProb
	}

	metrics.SetMRXVenueSpreadBPS(instrumentKey, merged.GlobalSpreadBPS)
	metrics.SetMRXVenueDivergenceBPS(instrumentKey, merged.VenueDivergenceBPS)
	metrics.SetMRXVenueVenuesActive(instrumentKey, len(merged.BestBids))

	seq := p.nextCrossVenueSeq(instrumentKey)
	outEnv, prob := buildCrossVenueBookEnvelope(env, instrumentKey, seq, merged)
	if prob != nil {
		return prob
	}
	if prob := p.cfg.PublishEnvelope.Publish(context.Background(), outEnv); prob != nil {
		return prob
	}
	return nil
}

func (p *ProcessorSubsystemActor) upsertCrossVenueBook(instrumentKey string, book aggdomain.CrossVenueVenueBook) {
	if p.crossVenueBooks == nil {
		p.crossVenueBooks = make(map[string]map[string]aggdomain.CrossVenueVenueBook)
	}
	if p.crossVenueSeq == nil {
		p.crossVenueSeq = make(map[string]int64)
	}
	venues, ok := p.crossVenueBooks[instrumentKey]
	if !ok {
		p.evictCrossVenueInstrumentIfNeeded()
		venues = make(map[string]aggdomain.CrossVenueVenueBook, p.cfg.CrossVenue.MaxVenues)
		p.crossVenueBooks[instrumentKey] = venues
		p.crossVenueInstrumentQ = append(p.crossVenueInstrumentQ, instrumentKey)
	}

	if _, exists := venues[book.Venue]; !exists && len(venues) >= p.cfg.CrossVenue.MaxVenues {
		evictKey, found := deterministicCrossVenueEvictionCandidate(venues)
		if found && strings.Compare(book.Venue, evictKey) < 0 {
			delete(venues, evictKey)
		} else {
			return
		}
	}
	venues[book.Venue] = book
}

func (p *ProcessorSubsystemActor) evictCrossVenueInstrumentIfNeeded() {
	if p.cfg.CrossVenue.MaxInstruments <= 0 {
		return
	}
	if len(p.crossVenueBooks) < p.cfg.CrossVenue.MaxInstruments {
		return
	}
	if len(p.crossVenueInstrumentQ) == 0 {
		return
	}
	evicted := p.crossVenueInstrumentQ[0]
	p.crossVenueInstrumentQ = p.crossVenueInstrumentQ[1:]
	delete(p.crossVenueBooks, evicted)
	delete(p.crossVenueSeq, evicted)
	metrics.SetMRXVenueVenuesActive(evicted, 0)
}

func deterministicCrossVenueEvictionCandidate(venues map[string]aggdomain.CrossVenueVenueBook) (string, bool) {
	var (
		evict string
		found bool
	)
	for venue := range venues {
		if !found || strings.Compare(venue, evict) > 0 {
			evict = venue
			found = true
		}
	}
	return evict, found
}

func (p *ProcessorSubsystemActor) collectCrossVenueBooks(instrumentKey string) []aggdomain.CrossVenueVenueBook {
	venues := p.crossVenueBooks[instrumentKey]
	if len(venues) == 0 {
		return nil
	}
	venueKeys := make([]string, 0, len(venues))
	for venue := range venues {
		venueKeys = append(venueKeys, venue)
	}
	sort.Strings(venueKeys)
	books := make([]aggdomain.CrossVenueVenueBook, 0, len(venueKeys))
	for _, venue := range venueKeys {
		books = append(books, venues[venue])
	}
	return books
}

func sortedOrderBookKeys(active map[aggdomain.BookID]struct{}) []aggdomain.BookID {
	if len(active) == 0 {
		return nil
	}
	keys := make([]aggdomain.BookID, 0, len(active))
	for key := range active {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Venue != keys[j].Venue {
			return strings.Compare(keys[i].Venue, keys[j].Venue) < 0
		}
		return strings.Compare(keys[i].Instrument, keys[j].Instrument) < 0
	})
	return keys
}

func sortedHeatmapKeys(active map[insightsapp.HeatmapSnapshotKey]struct{}) []insightsapp.HeatmapSnapshotKey {
	if len(active) == 0 {
		return nil
	}
	keys := make([]insightsapp.HeatmapSnapshotKey, 0, len(active))
	for key := range active {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Venue != keys[j].Venue {
			return strings.Compare(keys[i].Venue, keys[j].Venue) < 0
		}
		if keys[i].Instrument != keys[j].Instrument {
			return strings.Compare(keys[i].Instrument, keys[j].Instrument) < 0
		}
		return strings.Compare(keys[i].Timeframe, keys[j].Timeframe) < 0
	})
	return keys
}

func sortedVolumeKeys(active map[insightsapp.VolumeProfileSnapshotKey]struct{}) []insightsapp.VolumeProfileSnapshotKey {
	if len(active) == 0 {
		return nil
	}
	keys := make([]insightsapp.VolumeProfileSnapshotKey, 0, len(active))
	for key := range active {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Venue != keys[j].Venue {
			return strings.Compare(keys[i].Venue, keys[j].Venue) < 0
		}
		if keys[i].Instrument != keys[j].Instrument {
			return strings.Compare(keys[i].Instrument, keys[j].Instrument) < 0
		}
		return strings.Compare(keys[i].Timeframe, keys[j].Timeframe) < 0
	})
	return keys
}

func (p *ProcessorSubsystemActor) nextCrossVenueSeq(instrumentKey string) int64 {
	next := p.crossVenueSeq[instrumentKey] + 1
	if next <= 0 {
		next = 1
	}
	p.crossVenueSeq[instrumentKey] = next
	return next
}

func (p *ProcessorSubsystemActor) handleTradeForInsights(env envelope.Envelope, trade mddomain.TradeTickV1) {
	if p.cfg.Insights == nil {
		return
	}

	instrument := stateInstrumentKey(env)
	for _, timeframe := range p.insightsTimeframes() {
		if p.cfg.Insights.Heatmap != nil {
			res := p.cfg.Insights.Heatmap.Execute(context.Background(), insightsapp.BuildHeatmapRequest{
				EventType:  env.Type,
				Venue:      env.Venue,
				Instrument: instrument,
				Timeframe:  timeframe,
				TickSize:   defaultInsightsTickSize,
				Price:      trade.Price,
				Size:       trade.Size,
				Side:       trade.Side,
				TsIngest:   env.TsIngest,
				Seq:        env.Seq,
			})
			if res.IsOk() {
				p.markHeatmapActive(env.Venue, instrument, timeframe)
			}
		}
		if p.cfg.Insights.VolumeProfile != nil {
			res := p.cfg.Insights.VolumeProfile.Execute(context.Background(), insightsapp.BuildVolumeProfileRequest{
				EventType:  env.Type,
				Venue:      env.Venue,
				Instrument: instrument,
				Timeframe:  timeframe,
				TickSize:   defaultInsightsTickSize,
				Price:      trade.Price,
				Size:       trade.Size,
				Side:       trade.Side,
				TsIngest:   env.TsIngest,
				Seq:        env.Seq,
			})
			if res.IsOk() && res.Value().Emitted {
				p.markVolumeActive(env.Venue, instrument, timeframe)
			}
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
		metrics.IncIngestDrop("stats_route_disabled")
		return nil
	}
	if p.cfg.Service == nil || p.cfg.Service.Stats == nil {
		p.logger.Warn("aggruntime: no Stats use case configured — dropping liquidation")
		metrics.IncIngestDrop("stats_route_unconfigured")
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
		metrics.IncIngestDrop("stats_route_disabled")
		return nil
	}
	if p.cfg.Service == nil || p.cfg.Service.Stats == nil {
		p.logger.Warn("aggruntime: no Stats use case configured — dropping markprice")
		metrics.IncIngestDrop("stats_route_unconfigured")
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
	if mark.FundingRate != 0 && p.cfg.Service != nil && p.cfg.Service.Funding != nil {
		resp, prob = p.cfg.Service.Funding.Execute(context.Background(), env.Venue, stateInstrumentKey(env), env.Seq, env.TsIngest, mark)
		if prob != nil {
			if isBenignStreamOrderProblem(prob) {
				p.logger.Debug("aggruntime: BuildFunding ignored stale funding",
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

func (p *ProcessorSubsystemActor) insightsTimeframes() []string {
	if len(p.cfg.InsightsTimeframes) > 0 {
		return p.cfg.InsightsTimeframes
	}
	return []string{defaultInsightsTimeframe}
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
	if p.shouldDeferSnapshotTick(p.clockNow(), msg.Kind) {
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

func (p *ProcessorSubsystemActor) shouldDeferSnapshotTick(now time.Time, kind SnapshotTickKind) bool {
	if p.hbLastTsIngest <= 0 {
		return false
	}
	nowMs := now.UnixMilli()
	if nowMs <= p.hbLastTsIngest {
		return false
	}
	skew := time.Duration(nowMs-p.hbLastTsIngest) * time.Millisecond
	if skew <= snapshotTickDeferSkewThreshold {
		return false
	}
	if shouldEmitHeartbeat(now, p.snapshotTickDeferLastLogAt, false, snapshotTickDeferLogInterval) {
		p.snapshotTickDeferLastLogAt = now
		p.logger.Info("aggruntime: deferring periodic snapshot tick while processor catches up",
			"kind", msgKindString(kind),
			"ingest_skew", skew.String(),
			"last_ts_ingest", p.hbLastTsIngest,
		)
	}
	return true
}

func (p *ProcessorSubsystemActor) shouldSkipBookDeltaForCatchUp(env envelope.Envelope) bool {
	if p.cfg.CatchUpSkipBookDeltaSkew <= 0 || env.TsIngest <= 0 || p.hbLastTsIngest <= 0 {
		return false
	}
	if p.hbLastTsIngest <= env.TsIngest {
		return false
	}
	skew := time.Duration(p.hbLastTsIngest-env.TsIngest) * time.Millisecond
	if skew <= p.cfg.CatchUpSkipBookDeltaSkew {
		return false
	}
	now := p.clockNow()
	if shouldEmitHeartbeat(now, p.bookDeltaCatchUpSkipLastLogAt, false, snapshotTickDeferLogInterval) {
		p.bookDeltaCatchUpSkipLastLogAt = now
		p.logger.Info("aggruntime: skipping stale bookdelta while processor catches up",
			"ingest_skew", skew.String(),
			"threshold", p.cfg.CatchUpSkipBookDeltaSkew.String(),
			"watermark_ts_ingest", p.hbLastTsIngest,
			"envelope_ts_ingest", env.TsIngest,
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
		)
	}
	metrics.IncIngestDrop("bookdelta_catchup_skip")
	return true
}

func (p *ProcessorSubsystemActor) shouldSkipTradeForCatchUp(env envelope.Envelope) bool {
	if p.cfg.CatchUpSkipTradeSkew <= 0 || env.TsIngest <= 0 || p.hbLastTsIngest <= 0 {
		return false
	}
	if p.hbLastTsIngest <= env.TsIngest {
		return false
	}
	skew := time.Duration(p.hbLastTsIngest-env.TsIngest) * time.Millisecond
	if skew <= p.cfg.CatchUpSkipTradeSkew {
		return false
	}
	now := p.clockNow()
	if shouldEmitHeartbeat(now, p.tradeCatchUpSkipLastLogAt, false, snapshotTickDeferLogInterval) {
		p.tradeCatchUpSkipLastLogAt = now
		p.logger.Info("aggruntime: skipping stale trade while processor catches up",
			"ingest_skew", skew.String(),
			"threshold", p.cfg.CatchUpSkipTradeSkew.String(),
			"watermark_ts_ingest", p.hbLastTsIngest,
			"envelope_ts_ingest", env.TsIngest,
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
		)
	}
	metrics.IncIngestDrop("trade_catchup_skip")
	return true
}

func (p *ProcessorSubsystemActor) shouldSkipLiquidationForCatchUp(env envelope.Envelope) bool {
	if p.cfg.CatchUpSkipStatsSkew <= 0 || env.TsIngest <= 0 || p.hbLastTsIngest <= 0 {
		return false
	}
	if p.hbLastTsIngest <= env.TsIngest {
		return false
	}
	skew := time.Duration(p.hbLastTsIngest-env.TsIngest) * time.Millisecond
	if skew <= p.cfg.CatchUpSkipStatsSkew {
		return false
	}
	now := p.clockNow()
	if shouldEmitHeartbeat(now, p.liquidationCatchUpSkipLogAt, false, snapshotTickDeferLogInterval) {
		p.liquidationCatchUpSkipLogAt = now
		p.logger.Info("aggruntime: skipping stale liquidation while processor catches up",
			"ingest_skew", skew.String(),
			"threshold", p.cfg.CatchUpSkipStatsSkew.String(),
			"watermark_ts_ingest", p.hbLastTsIngest,
			"envelope_ts_ingest", env.TsIngest,
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
		)
	}
	metrics.IncIngestDrop("liquidation_catchup_skip")
	return true
}

func (p *ProcessorSubsystemActor) shouldSkipMarkPriceForCatchUp(env envelope.Envelope) bool {
	if p.cfg.CatchUpSkipStatsSkew <= 0 || env.TsIngest <= 0 || p.hbLastTsIngest <= 0 {
		return false
	}
	if p.hbLastTsIngest <= env.TsIngest {
		return false
	}
	skew := time.Duration(p.hbLastTsIngest-env.TsIngest) * time.Millisecond
	if skew <= p.cfg.CatchUpSkipStatsSkew {
		return false
	}
	now := p.clockNow()
	if shouldEmitHeartbeat(now, p.markPriceCatchUpSkipLogAt, false, snapshotTickDeferLogInterval) {
		p.markPriceCatchUpSkipLogAt = now
		p.logger.Info("aggruntime: skipping stale markprice while processor catches up",
			"ingest_skew", skew.String(),
			"threshold", p.cfg.CatchUpSkipStatsSkew.String(),
			"watermark_ts_ingest", p.hbLastTsIngest,
			"envelope_ts_ingest", env.TsIngest,
			"venue", env.Venue,
			"instrument", env.Instrument,
			"seq", env.Seq,
		)
	}
	metrics.IncIngestDrop("markprice_catchup_skip")
	return true
}

func msgKindString(kind SnapshotTickKind) string {
	if kind == "" {
		return "unknown"
	}
	return string(kind)
}

func (p *ProcessorSubsystemActor) publishOrderBookSnapshots() {
	if p.cfg.Service == nil {
		return
	}
	for _, key := range sortedOrderBookKeys(p.activeOrderBooks) {
		res := p.cfg.Service.SnapshotOrderBook(context.Background(), key)
		if res.IsFail() {
			continue
		}
		env, prob := buildOrderbookSnapshotEnvelope(res.Value(), p.publishTimestampMs())
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
	for _, key := range sortedHeatmapKeys(p.activeHeatmaps) {
		res := p.cfg.Insights.SnapshotHeatmap(context.Background(), key)
		if res.IsFail() {
			continue
		}
		env, prob := buildHeatmapSnapshotEnvelope(res.Value(), p.publishTimestampMs())
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
	for _, key := range sortedVolumeKeys(p.activeVolumes) {
		res := p.cfg.Insights.SnapshotVolumeProfile(context.Background(), key)
		if res.IsFail() {
			continue
		}
		env, prob := buildVolumeSnapshotEnvelope(res.Value(), p.publishTimestampMs())
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
		Instrument:  naming.StripMarketType(snapshot.BookID.Instrument),
		TsIngest:    nowMs,
		Seq:         snapshot.Seq,
		ContentType: envelope.ContentTypeJSON,
		Payload:     payload,
		IdempotencyKey: sharedhash.HashFieldsFast(
			"aggregation.snapshot",
			snapshot.BookID.Venue,
			snapshot.BookID.Instrument,
			strconv.FormatInt(snapshot.Seq, 10),
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
	idempotencyKey := insightsapp.HeatmapArtifactIdempotencyKey(snapshot)
	if idempotencyKey == "" {
		seq := heatmapSeq(snapshot)
		idempotencyKey = sharedhash.HashFieldsFast(
			insightsdomain.HeatmapSnapshotType,
			snapshot.Venue,
			snapshot.Instrument,
			snapshot.Timeframe,
			strconv.FormatInt(snapshot.WindowStartTs, 10),
			strconv.FormatInt(seq, 10),
		)
	}

	out := envelope.Envelope{
		Type:           insightsdomain.HeatmapSnapshotType,
		Version:        insightsdomain.HeatmapSnapshotVersion,
		Venue:          snapshot.Venue,
		Instrument:     naming.StripMarketType(snapshot.Instrument),
		TsIngest:       nowMs,
		Seq:            heatmapSeq(snapshot),
		ContentType:    ct,
		Meta:           map[string]string{metaKeyTimeframe: snapshot.Timeframe},
		Payload:        payload,
		IdempotencyKey: idempotencyKey,
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
		Instrument:  naming.StripMarketType(snapshot.Instrument),
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
			strconv.FormatInt(snapshot.WindowEndTs, 10),
			strconv.FormatInt(volumeSeq(snapshot), 10),
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
			strconv.Itoa(insightsdomain.CrossVenueTradeSnapshotVersion),
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
			strconv.Itoa(insightsdomain.CrossVenueSpreadSignalVersion),
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

func buildCrossVenueBookEnvelope(
	trigger envelope.Envelope,
	instrumentKey string,
	seq int64,
	snapshot aggdomain.CrossVenueBookSnapshotV1,
) (envelope.Envelope, *problem.Problem) {
	if seq <= 0 {
		return envelope.Envelope{}, problem.New(problem.ValidationFailed, "cross-venue seq must be > 0")
	}
	ct := resolveContentType(typeXVenueBook)
	payload, p := codec.EncodePayload(
		typeXVenueBook,
		xVenueBookVersion,
		ct,
		snapshot,
	)
	if p != nil {
		return envelope.Envelope{}, p
	}

	meta := make(map[string]string, 1)
	if marketType := envelopeMarketType(trigger); marketType != "" {
		meta[metaKeyMarketType] = marketType
	}
	if len(meta) == 0 {
		meta = nil
	}

	out := envelope.Envelope{
		Type:        typeXVenueBook,
		Version:     xVenueBookVersion,
		Venue:       xVenueBookVenue,
		Instrument:  naming.StripMarketType(instrumentKey),
		TsExchange:  trigger.TsExchange,
		TsIngest:    snapshot.TsServerMs,
		Seq:         seq,
		ContentType: ct,
		Meta:        meta,
		Payload:     payload,
		IdempotencyKey: sharedhash.HashFieldsFast(
			typeXVenueBook,
			strconv.Itoa(xVenueBookVersion),
			strings.ToUpper(strings.TrimSpace(instrumentKey)),
			strconv.FormatInt(seq, 10),
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
	now := p.clockNow()
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

func (p *ProcessorSubsystemActor) clockNow() time.Time {
	if p.cfg.Now != nil {
		return p.cfg.Now()
	}
	return time.Now()
}

// publishTimestampMs returns a deterministic publish timestamp for timer-driven
// snapshots. Using the latest observed ingest watermark keeps replay output
// byte-identical without depending on wall clock.
func (p *ProcessorSubsystemActor) publishTimestampMs() int64 {
	if p.hbLastTsIngest > 0 {
		return p.hbLastTsIngest
	}
	return 1
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
		// Avoid fmt.Sprintf allocation on hot-path; use strconv for integer conversion.
		out[i] = entries[i].k + ":" + strconv.FormatInt(entries[i].v, 10)
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
