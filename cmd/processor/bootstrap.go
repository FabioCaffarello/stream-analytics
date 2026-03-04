package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anthdm/hollywood/actor"
	aggruntime "github.com/market-raccoon/internal/actors/aggregation/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	signalruntime "github.com/market-raccoon/internal/actors/signal/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	adapterstorage "github.com/market-raccoon/internal/adapters/storage"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	"github.com/market-raccoon/internal/adapters/storage/timescale"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	insightsapp "github.com/market-raccoon/internal/core/insights/app"
	mddomain "github.com/market-raccoon/internal/core/marketdata/domain"
	signalcore "github.com/market-raccoon/internal/core/signal"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/codec"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
	"github.com/market-raccoon/internal/shared/shardregistry"
)

// ---------------------------------------------------------------------------
// stub adapters for aggregation ports
// ---------------------------------------------------------------------------

type logArtifactPublisher struct{ logger *slog.Logger }

func (p *logArtifactPublisher) PublishSnapshot(_ context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	p.logger.Debug("aggregation: snapshot published",
		"venue", snap.BookID.Venue,
		"instrument", snap.BookID.Instrument,
		"seq", snap.Seq,
		"bids", len(snap.Bids),
		"asks", len(snap.Asks),
	)
	return nil
}

func (p *logArtifactPublisher) PublishInconsistent(_ context.Context, evt aggdomain.OrderBookInconsistentDetected) *problem.Problem {
	p.logger.Warn("aggregation: inconsistent book detected",
		"venue", evt.BookID.Venue,
		"instrument", evt.BookID.Instrument,
		"seq", evt.Seq,
		"reason", evt.Reason,
	)
	return nil
}

func (p *logArtifactPublisher) PublishCandleClosed(_ context.Context, evt aggdomain.CandleClosed) *problem.Problem {
	p.logger.Debug("aggregation: candle closed",
		"venue", evt.Candle.Venue,
		"instrument", evt.Candle.Instrument,
		"timeframe", evt.Candle.Timeframe,
		"window_start_ts", evt.Candle.WindowStartTs,
		"window_end_ts", evt.Candle.WindowEndTs,
		"trade_count", evt.Candle.TradeCount,
	)
	return nil
}

func (p *logArtifactPublisher) PublishStatsClosed(_ context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
	p.logger.Debug("aggregation: stats window closed",
		"venue", evt.Stats.Venue,
		"instrument", evt.Stats.Instrument,
		"timeframe", evt.Stats.Timeframe,
		"window_start_ts", evt.Stats.WindowStartTs,
		"window_end_ts", evt.Stats.WindowEndTs,
		"liq_count", evt.Stats.LiqCount,
	)
	return nil
}

func (p *logArtifactPublisher) PublishTapeClosed(_ context.Context, evt aggdomain.TapeClosed) *problem.Problem {
	p.logger.Debug("aggregation: tape window closed",
		"venue", evt.Window.Venue,
		"instrument", evt.Window.Instrument,
		"timeframe", evt.Window.Timeframe,
		"window_start_ts", evt.Window.WindowStartTs,
		"window_end_ts", evt.Window.WindowEndTs,
		"trade_count", evt.Window.TradeCount,
		"is_burst", evt.IsBurst,
	)
	return nil
}

type committedHotStore struct {
	committer *adapterstorage.SnapshotCommitter
}

func (s *committedHotStore) Save(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	if s == nil || s.committer == nil {
		return problem.New(problem.ValidationFailed, "committed hot store is not configured")
	}
	return s.committer.Commit(ctx, snap)
}

type logCandleHotStore struct{ logger *slog.Logger }

func (s *logCandleHotStore) SaveCandle(_ context.Context, evt aggdomain.CandleClosed) *problem.Problem {
	s.logger.Debug("aggregation: candle saved to hot store",
		"venue", evt.Candle.Venue,
		"instrument", evt.Candle.Instrument,
		"timeframe", evt.Candle.Timeframe,
		"trade_count", evt.Candle.TradeCount,
	)
	return nil
}

type logStatsHotStore struct{ logger *slog.Logger }

func (s *logStatsHotStore) SaveStats(_ context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
	s.logger.Debug("aggregation: stats saved to hot store",
		"venue", evt.Stats.Venue,
		"instrument", evt.Stats.Instrument,
		"timeframe", evt.Stats.Timeframe,
		"liq_count", evt.Stats.LiqCount,
	)
	return nil
}

type subMinuteRolloutGate struct {
	enabled     bool
	venues      map[string]struct{}
	instruments map[string]struct{}
}

func newSubMinuteRolloutGate(cfg config.ProcessorSubMinuteRolloutConfig) *subMinuteRolloutGate {
	gate := &subMinuteRolloutGate{
		enabled:     cfg.Enabled,
		venues:      make(map[string]struct{}, len(cfg.Venues)),
		instruments: make(map[string]struct{}, len(cfg.Instruments)),
	}
	for _, venue := range cfg.Venues {
		if v := strings.ToUpper(strings.TrimSpace(venue)); v != "" {
			gate.venues[v] = struct{}{}
		}
	}
	for _, instrument := range cfg.Instruments {
		if inst := strings.ToUpper(strings.TrimSpace(instrument)); inst != "" {
			gate.instruments[inst] = struct{}{}
		}
	}
	return gate
}

func (g *subMinuteRolloutGate) allows(venue, instrument, timeframe string) bool {
	if g == nil || !isSubMinuteTimeframe(timeframe) {
		return true
	}
	if !g.enabled {
		return false
	}
	if len(g.venues) > 0 {
		if _, ok := g.venues[strings.ToUpper(strings.TrimSpace(venue))]; !ok {
			return false
		}
	}
	if len(g.instruments) > 0 {
		raw := strings.ToUpper(strings.TrimSpace(instrument))
		if _, ok := g.instruments[raw]; ok {
			return true
		}
		base := raw
		if idx := strings.Index(base, ":"); idx > 0 {
			base = base[:idx]
		}
		if _, ok := g.instruments[base]; !ok {
			return false
		}
	}
	return true
}

func isSubMinuteTimeframe(timeframe string) bool {
	switch strings.ToLower(strings.TrimSpace(timeframe)) {
	case "1s", "5s":
		return true
	default:
		return false
	}
}

type subMinuteFilteringArtifactPublisher struct {
	next aggports.ArtifactPublisher
	gate *subMinuteRolloutGate
}

func (p *subMinuteFilteringArtifactPublisher) PublishSnapshot(ctx context.Context, snap aggdomain.SnapshotProduced) *problem.Problem {
	if p == nil || p.next == nil {
		return nil
	}
	return p.next.PublishSnapshot(ctx, snap)
}

func (p *subMinuteFilteringArtifactPublisher) PublishInconsistent(ctx context.Context, evt aggdomain.OrderBookInconsistentDetected) *problem.Problem {
	if p == nil || p.next == nil {
		return nil
	}
	return p.next.PublishInconsistent(ctx, evt)
}

func (p *subMinuteFilteringArtifactPublisher) PublishCandleClosed(ctx context.Context, evt aggdomain.CandleClosed) *problem.Problem {
	if p == nil || p.next == nil {
		return nil
	}
	if p.gate != nil && !p.gate.allows(evt.Candle.Venue, evt.Candle.Instrument, evt.Candle.Timeframe) {
		metrics.IncIngestDrop("subminute_rollout_blocked")
		return nil
	}
	return p.next.PublishCandleClosed(ctx, evt)
}

func (p *subMinuteFilteringArtifactPublisher) PublishStatsClosed(ctx context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
	if p == nil || p.next == nil {
		return nil
	}
	if p.gate != nil && !p.gate.allows(evt.Stats.Venue, evt.Stats.Instrument, evt.Stats.Timeframe) {
		metrics.IncIngestDrop("subminute_rollout_blocked")
		return nil
	}
	return p.next.PublishStatsClosed(ctx, evt)
}

func (p *subMinuteFilteringArtifactPublisher) PublishTapeClosed(ctx context.Context, evt aggdomain.TapeClosed) *problem.Problem {
	if p == nil || p.next == nil {
		return nil
	}
	return p.next.PublishTapeClosed(ctx, evt)
}

type subMinuteFilteringCandleStore struct {
	next aggports.CandleHotReadModelStore
	gate *subMinuteRolloutGate
}

func (s *subMinuteFilteringCandleStore) SaveCandle(ctx context.Context, evt aggdomain.CandleClosed) *problem.Problem {
	if s == nil || s.next == nil {
		return nil
	}
	if s.gate != nil && !s.gate.allows(evt.Candle.Venue, evt.Candle.Instrument, evt.Candle.Timeframe) {
		return nil
	}
	return s.next.SaveCandle(ctx, evt)
}

type subMinuteFilteringStatsStore struct {
	next aggports.StatsHotReadModelStore
	gate *subMinuteRolloutGate
}

func (s *subMinuteFilteringStatsStore) SaveStats(ctx context.Context, evt aggdomain.StatsWindowClosed) *problem.Problem {
	if s == nil || s.next == nil {
		return nil
	}
	if s.gate != nil && !s.gate.allows(evt.Stats.Venue, evt.Stats.Instrument, evt.Stats.Timeframe) {
		return nil
	}
	return s.next.SaveStats(ctx, evt)
}

// ---------------------------------------------------------------------------
// envelope source abstraction
// ---------------------------------------------------------------------------

type envelopeSource struct {
	envelopeCh <-chan envelope.Envelope
	consumeErr <-chan *problem.Problem
	shutdownFn func(context.Context)
	onResult   func(aggruntime.EnvelopeProcessResult)
}

func fanOutEnvelopeStream(source <-chan envelope.Envelope, capacity int) (<-chan envelope.Envelope, <-chan envelope.Envelope) {
	if capacity <= 0 {
		capacity = 1024
	}
	aggregationCh := make(chan envelope.Envelope, capacity)
	signalCh := make(chan envelope.Envelope, capacity)
	go func() {
		defer close(aggregationCh)
		defer close(signalCh)
		if source == nil {
			return
		}
		for env := range source {
			aggregationCh <- env
			signalCh <- env
		}
	}()
	return aggregationCh, signalCh
}

func buildSignalEngineConfig(cfg config.AppConfig) signalcore.EngineConfig {
	out := signalcore.DefaultEngineConfig()
	if cfg.Signals.WindowCap > 0 {
		out.Store.PerStreamWindow = cfg.Signals.WindowCap
	}
	if cfg.Evidence.RegimeMaxStreams > 0 {
		out.Store.PerTenantStreamCap = cfg.Evidence.RegimeMaxStreams
	}
	if cfg.Processor.MaxInstruments > 0 {
		out.Store.GlobalStreamCap = cfg.Processor.MaxInstruments
	}
	if cfg.Signals.DedupWindowMs > 0 {
		out.Store.DedupWindowMillis = cfg.Signals.DedupWindowMs
	}
	if cfg.Signals.CorrelationWindowMs > 0 {
		out.Rules.RegimeChange.WindowMs = cfg.Signals.CorrelationWindowMs
		out.Rules.LiquidityCollapse.WindowMs = cfg.Signals.CorrelationWindowMs
		out.Rules.PersistentImbalance.WindowMs = cfg.Signals.CorrelationWindowMs
	}
	return out
}

// ---------------------------------------------------------------------------
// Run — processor composition root
// ---------------------------------------------------------------------------

// Run is the processor composition root.  It wires the aggregation use case,
// envelope source, guardian, and blocks until a signal or error.
//
//nolint:gocyclo // composition root wires many runtime branches by design.
func Run(ctx context.Context, cfg config.AppConfig, configPath string) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)
	contracts.SetProtoRolloutConfig(cfg.ProtoRollout.EventTypeFlags())
	metrics.SetShardTopologyComplete(false)
	metrics.SetShardLeaseAgeSeconds(0)

	e2e, p := newE2ERuntime(logger)
	if p != nil {
		return fmt.Errorf("invalid e2e runtime posture: %v", p)
	}
	if p := e2e.startProbe(); p != nil {
		return fmt.Errorf("failed to start e2e probe: %v", p)
	}

	logger.Info("processor starting",
		"shard", fmt.Sprintf("%d/%d", cfg.Shard.Index, cfg.Shard.Count),
		"bus_type", cfg.Bus.Type,
	)

	var (
		registryConn          interface{ Close() }
		shardLease            shardregistry.Lease
		leaseLostErr          error
		leaseHeartbeatCancel  context.CancelFunc
		leaseLostCh           = make(chan error, 1)
		shardRegistryEnabled  = cfg.Shard.Registry.Enabled
		shardRegistryStrict   = cfg.Shard.Registry.Strict
		shardRegistryGraceDur = cfg.Shard.Registry.TopologyGraceDuration()
	)
	if shardRegistryEnabled {
		logger.Info("processor: shard registry enabled",
			"strict", shardRegistryStrict,
			"grace", shardRegistryGraceDur.String(),
			"bucket", shardregistry.DefaultBucket,
		)
		registry, nc, err := shardregistry.NewJetStreamKV(context.Background(), cfg.JetStream.URL, shardregistry.DefaultBucket, shardregistry.Config{
			LeaseTTL:          shardregistry.DefaultLeaseTTL,
			HeartbeatInterval: shardregistry.DefaultHeartbeatInterval,
		})
		if err != nil {
			metrics.IncShardRegistryError("init")
			return fmt.Errorf("processor: shard registry init failed: %w", err)
		}
		registryConn = nc

		instanceID := fmt.Sprintf("%s:%d:%d", strings.TrimSpace(cfg.JetStream.ConsumerDurable), os.Getpid(), time.Now().UnixNano())
		lease, err := registry.Acquire(context.Background(), cfg.Shard.Index, cfg.Shard.Count, instanceID)
		if err != nil {
			metrics.IncShardRegistryError("acquire")
			registryConn.Close()
			return fmt.Errorf("processor: shard lease acquire failed: %w", err)
		}
		shardLease = lease
		logger.Info("processor: shard lease acquired",
			"shard", fmt.Sprintf("%d/%d", cfg.Shard.Index, cfg.Shard.Count),
			"instance_id", instanceID,
		)

		hbCtx, hbCancel := context.WithCancel(context.Background())
		leaseHeartbeatCancel = hbCancel
		shardLease.StartHeartbeat(hbCtx, func(err error) {
			select {
			case leaseLostCh <- err:
			default:
			}
		})

		complete, err := registry.WaitForTopology(context.Background(), cfg.Shard.Count, shardRegistryGraceDur)
		if err != nil {
			metrics.IncShardRegistryError("topology_wait")
			hbCancel()
			_ = shardLease.Release(context.Background())
			registryConn.Close()
			return fmt.Errorf("processor: shard topology check failed: %w", err)
		}
		metrics.SetShardTopologyComplete(complete)
		if !complete {
			msg := fmt.Sprintf("processor: shard topology incomplete after grace=%s shard_count=%d", shardRegistryGraceDur, cfg.Shard.Count)
			if shardRegistryStrict {
				hbCancel()
				_ = shardLease.Release(context.Background())
				registryConn.Close()
				return fmt.Errorf("%s", msg)
			}
			logger.Warn(msg)
		} else {
			logger.Info("processor: shard topology complete", "shard_count", cfg.Shard.Count)
		}
	}

	// Bootstrap payload codec registry.
	if p := contracts.BootstrapPayloadCodecRegistryWithOptions(contracts.PayloadRegistryOptions{
		EnableInsightsVolumeProfileSnapshotProto: cfg.Processor.Insights.EnableVolumeProfileSnapshotProto,
	}); p != nil {
		return fmt.Errorf("payload codec registry bootstrap: %v", p)
	}

	// ── JetStream publisher (shared for artifacts + insights) ───────────
	var artifactPub aggports.ArtifactPublisher = &logArtifactPublisher{logger: logger}
	var jsPub *adapterjs.Publisher
	if strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
		pub, p := adapterjs.NewPublisher(context.Background(), adapterjs.PublisherConfig{
			URL:            cfg.JetStream.URL,
			StreamName:     cfg.JetStream.StreamName,
			DedupWindow:    cfg.JetStream.DedupWindowDuration(),
			MaxAge:         cfg.JetStream.MaxAgeDuration(),
			MaxBytes:       cfg.JetStream.MaxBytesInt64(),
			PublishTimeout: cfg.Processor.PublisherTimeoutDuration(),
		}, metrics.NewBusObserver())
		if p != nil {
			return fmt.Errorf("jetstream publisher init failed: %v", p)
		}
		jsPub = pub
		artifactPub = adapterjs.NewArtifactPublisher(pub, logger)
		logger.Info("processor: using jetstream artifact publisher",
			"url", cfg.JetStream.URL,
			"stream", cfg.JetStream.StreamName,
		)
	}

	// ── aggregation use case ────────────────────────────────────────────
	timescale.SetStubMode(timescale.AdapterModeStubMemory)

	var hotWriter aggports.HotReadModelStore = timescale.NewWriter()
	var candleStore aggports.CandleHotReadModelStore = &logCandleHotStore{logger: logger}
	var statsStore aggports.StatsHotReadModelStore = &logStatsHotStore{logger: logger}
	var heatmapStore *timescale.HeatmapWriter
	var volumeProfileStore *timescale.VolumeProfileWriter
	var tsPool *timescale.Pool
	if cfg.Storage.Timescale.Enabled {
		maxConns, err := int32FromConfig(cfg.Storage.Timescale.MaxConns, "storage.timescale.max_conns")
		if err != nil {
			return err
		}
		minConns, err := int32FromConfig(cfg.Storage.Timescale.MinConns, "storage.timescale.min_conns")
		if err != nil {
			return err
		}
		pool, p := timescale.NewPool(ctx, timescale.PoolConfig{
			DSN:               cfg.Storage.Timescale.DSN,
			MaxConns:          maxConns,
			MinConns:          minConns,
			MaxConnLifetime:   cfg.Storage.Timescale.MaxConnLifetimeDuration(),
			MaxConnIdleTime:   cfg.Storage.Timescale.MaxConnIdleTimeDuration(),
			HealthCheckPeriod: cfg.Storage.Timescale.HealthCheckPeriodDuration(),
		})
		if p != nil {
			return fmt.Errorf("timescale pool init failed: %v", p)
		}
		tsPool = pool
		defer tsPool.Close()

		hotWriter = timescale.NewPgWriter(tsPool)
		candleStore = timescale.NewPgCandleWriter(tsPool)
		statsStore = timescale.NewPgStatsWriter(tsPool)
		heatmapStore = timescale.NewPgHeatmapWriter(tsPool)
		volumeProfileStore = timescale.NewPgVolumeProfileWriter(tsPool)
		timescale.SetProductionReady(timescale.AdapterModePGX)
		logger.Info("processor: using Timescale pgx writer")
	} else {
		logger.Warn("processor: using in-memory Timescale writer (storage.timescale.enabled=false)")
	}

	var coldWriter aggports.ColdReadModelStore = clickhouse.NewWriter()
	if cfg.Storage.ClickHouse.Enabled {
		chPool, p := clickhouse.NewPool(ctx, clickhouse.PoolConfig{
			Addrs:           cfg.Storage.ClickHouse.Addrs,
			Database:        cfg.Storage.ClickHouse.Database,
			Username:        cfg.Storage.ClickHouse.Username,
			Password:        cfg.Storage.ClickHouse.Password,
			MaxOpenConns:    cfg.Storage.ClickHouse.MaxOpenConns,
			MaxIdleConns:    cfg.Storage.ClickHouse.MaxIdleConns,
			ConnMaxLifetime: cfg.Storage.ClickHouse.ConnMaxLifetimeDuration(),
			DialTimeout:     cfg.Storage.ClickHouse.DialTimeoutDuration(),
			ReadTimeout:     cfg.Storage.ClickHouse.ReadTimeoutDuration(),
		})
		if p != nil {
			return fmt.Errorf("clickhouse pool init failed: %v", p)
		}
		defer func() {
			if p := chPool.Close(); p != nil {
				logger.Warn("processor: clickhouse pool close failed", "err", p)
			}
		}()

		coldWriter = clickhouse.NewChWriter(chPool)
		logger.Info("processor: using ClickHouse writer")
	} else {
		logger.Warn("processor: using in-memory ClickHouse writer (storage.clickhouse.enabled=false)")
	}

	if !timescale.IsProductionReady() {
		logger.Warn("processor: timescale adapter running in non-production stub mode", "adapter_mode", timescale.AdapterMode())
	}

	hotStore := &committedHotStore{
		committer: adapterstorage.NewSnapshotCommitter(hotWriter, coldWriter),
	}
	subMinuteGate := newSubMinuteRolloutGate(cfg.Processor.SubMinuteRollout)
	artifactPub = &subMinuteFilteringArtifactPublisher{
		next: artifactPub,
		gate: subMinuteGate,
	}
	candleStore = &subMinuteFilteringCandleStore{
		next: candleStore,
		gate: subMinuteGate,
	}
	statsStore = &subMinuteFilteringStatsStore{
		next: statsStore,
		gate: subMinuteGate,
	}
	logger.Info("processor: sub-minute rollout gate configured",
		"enabled", subMinuteGate.enabled,
		"venue_allowlist", len(subMinuteGate.venues),
		"instrument_allowlist", len(subMinuteGate.instruments),
	)
	aggSvc := aggapp.NewAggregationService(aggapp.AggregationServiceConfig{
		Update: aggapp.UpdateConfig{
			MaxBooks:                   cfg.Processor.MaxInstruments,
			MaxLevels:                  cfg.Processor.OrderBook.MaxLevels,
			PublishDepthCap:            cfg.Processor.RTPublish.WsSnapshotDepthCap,
			SnapshotPublishMinInterval: time.Duration(cfg.Processor.RTPublish.OrderbookIntervalMs) * time.Millisecond,
		},
		Candle: aggapp.BuildCandleConfig{
			MaxCandles: cfg.Processor.Candle.MaxCandles,
			WindowCap:  cfg.Processor.Candle.WindowCap,
		},
		Stats: aggapp.BuildStatsConfig{
			MaxWindows: cfg.Processor.Stats.MaxWindows,
			WindowCap:  cfg.Processor.Stats.WindowCap,
		},
		Tape: aggapp.BuildTapeConfig{
			MaxWindows: cfg.Processor.Stats.MaxWindows,
			WindowCap:  cfg.Processor.Stats.WindowCap,
		},
		Publisher:   artifactPub,
		Store:       hotStore,
		CandleStore: candleStore,
		StatsStore:  statsStore,
	})

	var publishEnvelope aggruntime.EventPublisher
	if jsPub != nil {
		publishEnvelope = jsPub
	} else {
		publishEnvelope = bus.NewLogPublisher(logger)
	}

	insightsSvc := insightsapp.NewInsightsService(insightsapp.InsightsServiceConfig{
		VolumeProfile: insightsapp.BuildVolumeProfileConfig{},
		Heatmap:       insightsapp.BuildHeatmapConfig{},
		JoinTrades: insightsapp.JoinCrossVenueTradesConfig{
			MaxInstruments:     cfg.Processor.Insights.MaxInstruments,
			TTL:                cfg.Processor.Insights.TTLDuration(),
			EnableSpreadSignal: cfg.Processor.Insights.EnableSpreadSignal,
			MinVenues:          cfg.Processor.Insights.MinVenues,
			MinSpreadBPS:       cfg.Processor.Insights.MinSpreadBPS,
			RoundingMode:       cfg.Processor.Insights.RoundingMode,
			SweepEveryN:        cfg.Processor.Insights.SweepEveryN,
			SweepEvery:         cfg.Processor.Insights.SweepEveryDuration(),
		},
	})

	var joinTrades *insightsapp.JoinCrossVenueTrades
	if cfg.Processor.Insights.EnableCrossVenueJoin {
		joinTrades = insightsSvc.JoinTrades
		logger.Info("processor: cross-venue trade join enabled",
			"join_subject", cfg.Processor.Insights.JoinTradesSubject,
			"snapshot_subject_prefix", cfg.Processor.Insights.SnapshotSubjectPrefix,
			"max_instruments", cfg.Processor.Insights.MaxInstruments,
			"ttl", cfg.Processor.Insights.TTL,
			"enable_spread_signal", cfg.Processor.Insights.EnableSpreadSignal,
			"min_venues", cfg.Processor.Insights.MinVenues,
			"min_spread_bps", cfg.Processor.Insights.MinSpreadBPS,
			"rounding_mode", cfg.Processor.Insights.RoundingMode,
			"sweep_every_n", cfg.Processor.Insights.SweepEveryN,
			"sweep_every", cfg.Processor.Insights.SweepEvery,
		)
	}
	if cfg.Processor.XVenue.Enabled {
		logger.Info("processor: cross-venue orderbook merge enabled",
			"stale_threshold_ms", cfg.Processor.XVenue.StaleThresholdMs,
			"max_instruments", cfg.Processor.XVenue.MaxInstruments,
			"max_venues", cfg.Processor.XVenue.MaxVenues,
		)
	}

	// ── envelope source ─────────────────────────────────────────────────
	source := initEnvelopeSource(cfg, logger, e2e)
	aggregationInputCh, signalInputCh := fanOutEnvelopeStream(source.envelopeCh, cfg.Processor.BusCapacity)

	// ── processor subsystem config ──────────────────────────────────────
	processorCfg := aggruntime.ProcessorConfig{
		Logger:           logger,
		EnvelopeCh:       aggregationInputCh,
		Service:          aggSvc,
		CandleEnabled:    boolPtr(cfg.Processor.Candle.Enabled),
		StatsEnabled:     boolPtr(cfg.Processor.Stats.Enabled),
		Insights:         insightsSvc,
		JoinTrades:       joinTrades,
		CrossVenueMerger: aggdomain.DeterministicCrossVenueBookMerger{},
		CrossVenue: aggruntime.ProcessorCrossVenueConfig{
			Enabled:        cfg.Processor.XVenue.Enabled,
			StaleThreshold: time.Duration(cfg.Processor.XVenue.StaleThresholdMs) * time.Millisecond,
			MaxInstruments: cfg.Processor.XVenue.MaxInstruments,
			MaxVenues:      cfg.Processor.XVenue.MaxVenues,
		},
		PublishEnvelope:       publishEnvelope,
		HeatmapStore:          heatmapStore,
		VolumeProfileStore:    volumeProfileStore,
		SnapshotSubjectPrefix: cfg.Processor.Insights.SnapshotSubjectPrefix,
		RTPublish: aggruntime.ProcessorRTPublishConfig{
			OrderbookInterval:  time.Duration(cfg.Processor.RTPublish.OrderbookIntervalMs) * time.Millisecond,
			WsSnapshotDepthCap: cfg.Processor.RTPublish.WsSnapshotDepthCap,
			HeatmapInterval:    time.Duration(cfg.Processor.RTPublish.HeatmapIntervalMs) * time.Millisecond,
			VolumeInterval:     time.Duration(cfg.Processor.RTPublish.VolumeIntervalMs) * time.Millisecond,
		},
		CatchUpSkipBookDeltaSkew: time.Duration(cfg.Processor.CatchUpSkipBookDeltaSkewMs) * time.Millisecond,
		CatchUpSkipTradeSkew:     time.Duration(cfg.Processor.CatchUpSkipTradeSkewMs) * time.Millisecond,
		CatchUpSkipStatsSkew:     time.Duration(cfg.Processor.CatchUpSkipStatsSkewMs) * time.Millisecond,
		InsightsTimeframes:       cfg.Processor.Insights.InsightsTimeframes,
		OnEnvelopeProcessed:      source.onResult,
	}
	signalEngineCfg := buildSignalEngineConfig(cfg)
	signalCfg := signalruntime.SubsystemConfig{
		Logger:       logger,
		EnvelopeCh:   signalInputCh,
		Engine:       signalcore.NewSignalEngine(signalEngineCfg, nil),
		Publisher:    publishEnvelope,
		ReplicaID:    cfg.Shard.Index,
		ReplicaCount: cfg.Shard.Count,
	}

	// ── engine ──────────────────────────────────────────────────────────
	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		return err
	}

	// ── guardian with aggregation factory ────────────────────────────────
	guardianPID := actorruntime.SpawnGuardian(e, actorruntime.GuardianConfig{
		Logger: logger,
		Factories: map[actorruntime.Subsystem]actor.Producer{
			actorruntime.SubsystemAggregation: aggruntime.NewProcessorSubsystemActor(processorCfg),
			actorruntime.SubsystemSignals:     signalruntime.NewSubsystemActor(signalCfg),
		},
	})
	logger.Info("processor: guardian spawned", "pid", guardianPID.String())
	logger.Info("processor: waiting for envelopes (use cmd/consumer or inject via InMemoryBus)")

	if p := maybeInjectJoinFixture(cfg, e2e, publishEnvelope, logger); p != nil {
		return fmt.Errorf("failed to inject e2e join fixture: %v", p)
	}
	e2e.markReady()

	// ── HTTP server ─────────────────────────────────────────────────────
	srv := httpserver.NewServer(
		e,
		guardianPID,
		cfg.HTTP.Addr,
		cfg.HTTP.EnablePprof,
		logger,
		httpserver.WithTLS(cfg.HTTP.TLSCert, cfg.HTTP.TLSKey),
		httpserver.WithReloadHook(protoRolloutReloadHook(configPath, logger)),
	)

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// ── wait for signal or error ────────────────────────────────────────
	quit := bootstrap.SignalChannel()
	select {
	case <-quit:
	case err := <-serverErr:
		logger.Error("processor: HTTP server error", "err", err)
	case p := <-source.consumeErr:
		if p != nil {
			logger.Error("processor: jetstream consume loop failed", "err", p)
		}
	case err := <-leaseLostCh:
		leaseLostErr = err
		metrics.IncShardLeaseLost()
		logger.Error("processor: shard lease lost", "err", err)
	case <-ctx.Done():
	}

	// ── shutdown ────────────────────────────────────────────────────────
	logger.Info("processor: shutting down")

	httpShutCtx, httpShutCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer httpShutCancel()
	if err := srv.Shutdown(httpShutCtx); err != nil {
		logger.Warn("processor: HTTP shutdown error", "err", err)
	}

	depsCtx, depsCancel := context.WithTimeout(context.Background(), cfg.HTTP.GuardianShutdownTimeoutDuration())
	defer depsCancel()
	if p := e2e.shutdown(depsCtx); p != nil {
		logger.Warn("processor: e2e probe shutdown failed", "err", p)
	}
	if leaseHeartbeatCancel != nil {
		leaseHeartbeatCancel()
	}
	if shardLease != nil {
		if err := shardLease.Release(depsCtx); err != nil {
			logger.Warn("processor: shard lease release failed", "err", err)
		}
	}
	if shardRegistryEnabled {
		registryConn.Close()
	}
	source.shutdownFn(depsCtx)

	if jsPub != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), cfg.HTTP.PublisherFlushTimeoutDuration())
		if p := jsPub.Close(flushCtx); p != nil {
			logger.Warn("processor: jetstream publisher shutdown failed", "err", p)
		}
		flushCancel()
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.HTTP.GuardianShutdownTimeoutDuration())
	defer shutCancel()
	actorruntime.ShutdownGuardian(shutCtx, e, guardianPID, logger)
	logger.Info("processor: shutdown complete")
	if leaseLostErr != nil {
		return fmt.Errorf("processor: shard lease lost: %w", leaseLostErr)
	}
	return nil
}

// ---------------------------------------------------------------------------
// envelope source wiring
// ---------------------------------------------------------------------------

//nolint:gocyclo // runtime source selection intentionally handles many branches.
func initEnvelopeSource(cfg config.AppConfig, logger *slog.Logger, e2e *e2eRuntime) envelopeSource {
	replayMode := strings.ToLower(strings.TrimSpace(cfg.Replay.Mode))
	if replayMode == "" {
		replayMode = "off"
	}
	if replayMode == "off" && strings.TrimSpace(cfg.MarketData.ReplayPath) != "" {
		replayMode = "file"
	}

	switch replayMode {
	case "file":
		replayPath := strings.TrimSpace(cfg.MarketData.ReplayPath)
		if replayPath == "" {
			logger.Error("processor: replay.mode=file requires marketdata.replay_path")
			os.Exit(1)
		}
		return initReplayEnvelopeSource(replayPath, cfg.Processor.BusCapacity, logger)
	case "jetstream":
		return initJetStreamReplayEnvelopeSource(cfg, logger)
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Bus.Type)) {
	case "jetstream":
		ch := make(chan envelope.Envelope, cfg.Processor.BusCapacity)
		resultMailbox := newEnvelopeResultMailbox()
		resultWaitTimeout := cfg.JetStream.AckWaitDuration()
		if resultWaitTimeout <= 0 {
			resultWaitTimeout = 30 * time.Second
		}
		filterSubjects := effectiveJetStreamFilters(cfg)

		onResult := func(res aggruntime.EnvelopeProcessResult) {
			res.Problem = e2e.maybeInjectTransient(res.Envelope, res.Problem)
			resultMailbox.push(res)
		}

		durableName := shardAwareDurable(cfg.JetStream.ConsumerDurable, cfg.Shard.Index, cfg.Shard.Count)

		jetstreamConsumer, p := adapterjs.NewConsumer(context.Background(), adapterjs.ConsumerConfig{
			URL:             cfg.JetStream.URL,
			StreamName:      cfg.JetStream.StreamName,
			DedupWindow:     cfg.JetStream.DedupWindowDuration(),
			MaxAge:          cfg.JetStream.MaxAgeDuration(),
			MaxBytes:        cfg.JetStream.MaxBytesInt64(),
			ConsumerDurable: durableName,
			FilterSubjects:  filterSubjects,
			AckWait:         cfg.JetStream.AckWaitDuration(),
			MaxAckPending:   cfg.JetStream.MaxAckPending,
			MaxDeliver:      cfg.JetStream.MaxDeliver,
			DeliverPolicy:   cfg.JetStream.DeliverPolicy,
			ShardGroupCount: cfg.JetStream.ShardGroupCount,
			ShardGroupID:    cfg.JetStream.ShardGroupID,
			MaxLag:          cfg.Shard.MaxLag,
		}, metrics.NewBusObserver())
		if p != nil {
			logger.Error("processor: jetstream consumer init failed", "err", p)
			os.Exit(1)
		}

		runCtx, cancelConsume := context.WithCancel(context.Background())
		errCh := make(chan *problem.Problem, 1)
		go func() {
			errCh <- jetstreamConsumer.Consume(runCtx, func(consumeCtx context.Context, env envelope.Envelope) *problem.Problem {
				select {
				case ch <- env:
				case <-consumeCtx.Done():
					return problem.WithRetryable(problem.Wrap(consumeCtx.Err(), problem.Unavailable, "processor enqueue canceled"))
				}

				waitCtx, cancelWait := context.WithTimeout(consumeCtx, resultWaitTimeout)
				defer cancelWait()

				for {
					if res, ok := resultMailbox.pop(env); ok {
						metrics.IncProcessorAckAfterCommit(ackAfterCommitStatus(res.Problem))
						if res.Problem != nil && !res.Problem.Retryable {
							return nil // ACK non-retryable errors; retrying won't help
						}
						return res.Problem
					}

					select {
					case <-resultMailbox.wait():
						continue
					case <-waitCtx.Done():
						metrics.IncProcessorAckAfterCommit("failed")
						return problem.WithRetryable(problem.Wrap(waitCtx.Err(), problem.Unavailable, "processor result wait timed out"))
					}
				}
			})
		}()

		logger.Info("processor: subscribed to jetstream consumer",
			"url", cfg.JetStream.URL,
			"stream", cfg.JetStream.StreamName,
			"durable", durableName,
			"filters", filterSubjects,
			"shard", fmt.Sprintf("%d/%d", cfg.Shard.Index, cfg.Shard.Count),
		)

		return envelopeSource{
			envelopeCh: ch,
			consumeErr: errCh,
			onResult:   onResult,
			shutdownFn: func(shutCtx context.Context) {
				cancelConsume()
				if p := jetstreamConsumer.Close(shutCtx); p != nil {
					logger.Warn("processor: jetstream consumer close failed", "err", p)
				}
			},
		}
	default:
		eventBus := bus.NewInMemoryBus(cfg.Processor.BusCapacity, metrics.NewBusObserver())
		logger.Info("processor: subscribed to in-memory bus")
		return envelopeSource{
			envelopeCh: eventBus.Subscribe(),
			shutdownFn: func(context.Context) {
				eventBus.Close()
			},
		}
	}
}

type envelopeResultMailbox struct {
	mu      sync.Mutex
	byKey   map[string][]aggruntime.EnvelopeProcessResult
	notifyC chan struct{}
}

func newEnvelopeResultMailbox() *envelopeResultMailbox {
	return &envelopeResultMailbox{
		byKey:   make(map[string][]aggruntime.EnvelopeProcessResult),
		notifyC: make(chan struct{}, 1),
	}
}

func (m *envelopeResultMailbox) push(res aggruntime.EnvelopeProcessResult) {
	if m == nil {
		return
	}
	key := processedEnvelopeKey(res.Envelope)
	m.mu.Lock()
	m.byKey[key] = append(m.byKey[key], res)
	m.mu.Unlock()

	select {
	case m.notifyC <- struct{}{}:
	default:
	}
}

func (m *envelopeResultMailbox) pop(env envelope.Envelope) (aggruntime.EnvelopeProcessResult, bool) {
	if m == nil {
		return aggruntime.EnvelopeProcessResult{}, false
	}
	key := processedEnvelopeKey(env)
	m.mu.Lock()
	defer m.mu.Unlock()
	queue := m.byKey[key]
	if len(queue) == 0 {
		return aggruntime.EnvelopeProcessResult{}, false
	}
	res := queue[0]
	if len(queue) == 1 {
		delete(m.byKey, key)
	} else {
		m.byKey[key] = queue[1:]
	}
	return res, true
}

func (m *envelopeResultMailbox) wait() <-chan struct{} {
	if m == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return m.notifyC
}

func processedEnvelopeKey(env envelope.Envelope) string {
	return strings.ToLower(strings.TrimSpace(env.Type)) + "|" +
		strconv.Itoa(env.Version) + "|" +
		strings.ToLower(strings.TrimSpace(env.Venue)) + "|" +
		strings.ToLower(strings.TrimSpace(env.Instrument)) + "|" +
		strconv.FormatInt(env.Seq, 10) + "|" +
		strings.TrimSpace(env.IdempotencyKey)
}

func ackAfterCommitStatus(p *problem.Problem) string {
	if p == nil {
		return "ok"
	}
	return "failed"
}

func initJetStreamReplayEnvelopeSource(cfg config.AppConfig, logger *slog.Logger) envelopeSource {
	capacity := cfg.Processor.BusCapacity
	if capacity <= 0 {
		capacity = 1024
	}

	replayDurable := strings.TrimSpace(cfg.JetStream.ConsumerDurable) + "-replay"
	src, p := adapterjs.NewJetStreamReplaySource(adapterjs.ReplaySourceConfig{
		URL:             cfg.JetStream.URL,
		StreamName:      cfg.JetStream.StreamName,
		SubjectFilter:   cfg.Replay.JetStream.SubjectFilter,
		ConsumerDurable: replayDurable,
		DedupWindow:     cfg.JetStream.DedupWindowDuration(),
		MaxAge:          cfg.JetStream.MaxAgeDuration(),
		MaxBytes:        cfg.JetStream.MaxBytesInt64(),
		AckWait:         cfg.JetStream.AckWaitDuration(),
		MaxAckPending:   cfg.JetStream.MaxAckPending,
		MaxDeliver:      cfg.JetStream.MaxDeliver,
		DeliverPolicy:   cfg.Replay.JetStream.DeliverPolicy,
		Window:          cfg.Replay.JetStream.WindowDuration(),
		MaxMessages:     cfg.Replay.JetStream.MaxMessages,
		MergeBufferSize: cfg.Replay.JetStream.MergeBuffer,
		OutputBufferSize: func() int {
			if capacity < 256 {
				return capacity
			}
			return 256
		}(),
		DecodeErrorMode: cfg.Replay.OnDecodeError,
	})
	if p != nil {
		logger.Error("processor: jetstream replay source init failed", "err", p)
		os.Exit(1)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	sourceCh, closeFn, p := src.Read(runCtx)
	if p != nil {
		logger.Error("processor: jetstream replay source read init failed", "err", p)
		os.Exit(1)
	}

	outCh := make(chan envelope.Envelope, capacity)
	errCh := make(chan *problem.Problem, 1)
	var closeOnce sync.Once
	var closeProb *problem.Problem
	safeClose := func() *problem.Problem {
		closeOnce.Do(func() {
			if err := closeFn(); err != nil {
				closeProb = problem.WithRetryable(problem.Wrap(err, problem.Unavailable, "close jetstream replay source failed"))
			}
		})
		return closeProb
	}

	go func() {
		defer close(outCh)
		defer close(errCh)

		for {
			select {
			case <-runCtx.Done():
				errCh <- safeClose()
				return
			case env, ok := <-sourceCh:
				if !ok {
					errCh <- safeClose()
					return
				}

				select {
				case <-runCtx.Done():
					errCh <- safeClose()
					return
				case outCh <- env:
				}
			}
		}
	}()

	logger.Info("processor: replay source mode jetstream enabled",
		"url", cfg.JetStream.URL,
		"stream", cfg.JetStream.StreamName,
		"subject_filter", cfg.Replay.JetStream.SubjectFilter,
		"deliver_policy", cfg.Replay.JetStream.DeliverPolicy,
		"window", cfg.Replay.JetStream.Window,
		"max_messages", cfg.Replay.JetStream.MaxMessages,
		"merge_buffer", cfg.Replay.JetStream.MergeBuffer,
		"durable", replayDurable,
	)

	return envelopeSource{
		envelopeCh: outCh,
		consumeErr: errCh,
		shutdownFn: func(context.Context) {
			cancel()
			_ = safeClose()
		},
	}
}

func initReplayEnvelopeSource(path string, capacity int, logger *slog.Logger) envelopeSource {
	if capacity <= 0 {
		capacity = 1024
	}

	ch := make(chan envelope.Envelope, capacity)
	errCh := make(chan *problem.Problem, 1)
	runCtx, cancel := context.WithCancel(context.Background())

	go func() {
		defer close(ch)
		defer close(errCh)

		reader, p := replay.NewReader(path)
		if p != nil {
			errCh <- p
			return
		}
		defer func() {
			_ = reader.Close()
		}()

		count := 0
		hashes := make([]string, 0, 1024)
		for {
			rec, ok, p := reader.Next()
			if p != nil {
				errCh <- p
				return
			}
			if !ok {
				break
			}

			select {
			case <-runCtx.Done():
				errCh <- nil
				return
			case ch <- rec.Envelope:
				count++
				hashes = append(hashes, rec.SHA256)
			}
		}

		logger.Info("processor: replay fixture loaded",
			"replay_path", path,
			"records", count,
			"fingerprint", sharedhash.HashFieldsFast(hashes...),
		)
		errCh <- nil
	}()

	return envelopeSource{
		envelopeCh: ch,
		consumeErr: errCh,
		shutdownFn: func(context.Context) {
			cancel()
		},
	}
}

// ---------------------------------------------------------------------------
// publisher + JetStream helpers
// ---------------------------------------------------------------------------

func effectiveJetStreamFilters(cfg config.AppConfig) []string {
	base := append([]string(nil), cfg.JetStream.FilterSubjects...)
	base = appendFilterSubjectIfMissing(base, "insights.microstructure_evidence.v1.>")
	base = appendFilterSubjectIfMissing(base, "liquidity.evidence.v1.>")
	if cfg.Processor.Insights.EnableCrossVenueJoin {
		if joinSubject := strings.TrimSpace(cfg.Processor.Insights.JoinTradesSubject); joinSubject != "" {
			base = appendFilterSubjectIfMissing(base, joinSubject)
		}
	}
	if len(base) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(base))
	out := make([]string, 0, len(base))
	for _, raw := range base {
		subject := strings.TrimSpace(raw)
		if subject == "" {
			continue
		}
		if _, exists := seen[subject]; exists {
			continue
		}
		seen[subject] = struct{}{}
		out = append(out, subject)
	}
	return out
}

func appendFilterSubjectIfMissing(base []string, subject string) []string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return base
	}
	for _, existing := range base {
		if subjectMatchesFilter(subject, strings.TrimSpace(existing)) {
			return base
		}
	}
	return append(base, subject)
}

func subjectMatchesFilter(subject, filter string) bool {
	subject = strings.TrimSpace(subject)
	filter = strings.TrimSpace(filter)
	if subject == "" || filter == "" {
		return false
	}
	if filter == ">" {
		return true
	}
	if subject == filter {
		return true
	}
	if strings.HasSuffix(filter, ".>") {
		prefix := strings.TrimSuffix(filter, ">")
		return strings.HasPrefix(subject, prefix)
	}
	return false
}

func shardAwareDurable(base string, index, count int) string {
	if count <= 1 {
		return base
	}
	return fmt.Sprintf("%s-s%d", base, index)
}

func int32FromConfig(v int, field string) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, fmt.Errorf("%s out of int32 range: %d", field, v)
	}
	return int32(v), nil
}

func boolPtr(v bool) *bool {
	return &v
}

func protoRolloutReloadHook(configPath string, logger *slog.Logger) func() error {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil
	}
	return func() error {
		cfg, prob := config.Load(configPath)
		if prob != nil {
			return fmt.Errorf("reload config load failed: %v", prob)
		}
		if prob := cfg.Validate(); prob != nil {
			return fmt.Errorf("reload config validation failed: %v", prob)
		}
		contracts.SetProtoRolloutConfig(cfg.ProtoRollout.EventTypeFlags())
		logger.Info("processor: proto rollout flags reloaded", "config", configPath)
		return nil
	}
}

// ---------------------------------------------------------------------------
// E2E join fixture injection
// ---------------------------------------------------------------------------

func maybeInjectJoinFixture(cfg config.AppConfig, e2e *e2eRuntime, pub aggruntime.EventPublisher, logger *slog.Logger) *problem.Problem {
	if e2e == nil || !e2e.shouldInjectJoinFixture() {
		return nil
	}
	if !cfg.Processor.Insights.EnableCrossVenueJoin {
		logger.Warn("processor: skipping e2e join fixture injection because cross-venue join is disabled")
		return nil
	}
	if pub == nil {
		return problem.New(problem.ValidationFailed, "e2e join fixture injection requires publisher")
	}

	buildTrade := func(venue string, seq int64, tsIngest int64, price float64, side string, tradeID string) (envelope.Envelope, *problem.Problem) {
		payload, p := codec.EncodePayload("marketdata.trade", 1, envelope.ContentTypeJSON, mddomain.TradeTickV1{
			Price:     price,
			Size:      1.0,
			Side:      side,
			TradeID:   tradeID,
			Timestamp: tsIngest - 10,
		})
		if p != nil {
			return envelope.Envelope{}, p
		}
		env := envelope.Envelope{
			Type:           "marketdata.trade",
			Version:        1,
			Venue:          venue,
			Instrument:     e2e.joinInstrument,
			TsExchange:     tsIngest - 10,
			TsIngest:       tsIngest,
			Seq:            seq,
			IdempotencyKey: sharedhash.HashFieldsFast("e2e_join_fixture", venue, e2e.joinInstrument, side, tradeID),
			ContentType:    envelope.ContentTypeJSON,
			Meta: map[string]string{
				"instrument_market_type": "SPOT",
			},
			Payload: payload,
		}
		if p := env.Validate(); p != nil {
			return envelope.Envelope{}, p
		}
		return env, nil
	}

	trades := []struct {
		venue string
		seq   int64
		ts    int64
		price float64
		side  string
		id    string
	}{
		{venue: "BINANCE", seq: 1, ts: 1_710_000_001_000, price: 100.25, side: "buy", id: "e2e-b-1"},
		{venue: "BYBIT", seq: 1, ts: 1_710_000_001_010, price: 100.35, side: "sell", id: "e2e-y-1"},
	}
	for _, trade := range trades {
		env, p := buildTrade(trade.venue, trade.seq, trade.ts, trade.price, trade.side, trade.id)
		if p != nil {
			return p
		}
		if p := pub.Publish(context.Background(), env); p != nil {
			return p
		}
	}
	logger.Info("processor: injected deterministic e2e join fixture", "instrument", e2e.joinInstrument, "venues", len(trades))
	return nil
}
