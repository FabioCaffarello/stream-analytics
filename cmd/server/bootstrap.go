package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthdm/hollywood/actor"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	evidenceruntime "github.com/market-raccoon/internal/actors/evidence/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	signalsruntime "github.com/market-raccoon/internal/actors/signals/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	"github.com/market-raccoon/internal/adapters/storage/timescale"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	aggports "github.com/market-raccoon/internal/core/aggregation/ports"
	deliverydomain "github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	evidenceapp "github.com/market-raccoon/internal/core/evidence/app"
	evidencedomain "github.com/market-raccoon/internal/core/evidence/domain"
	signalsapp "github.com/market-raccoon/internal/core/signals/app"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	wsserver "github.com/market-raccoon/internal/interfaces/ws"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/ids"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

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

type deliveryEnvelopeRangeStore interface {
	ports.RangeStore
	StoreEnvelope(env envelope.Envelope)
}

type subMinuteFilteringRangeStore struct {
	next deliveryEnvelopeRangeStore
	gate *subMinuteRolloutGate
}

func (s subMinuteFilteringRangeStore) StoreEnvelope(env envelope.Envelope) {
	if s.next == nil {
		return
	}
	s.next.StoreEnvelope(env)
}

func (s subMinuteFilteringRangeStore) GetRange(
	ctx context.Context,
	subject deliverydomain.Subject,
	fromMs, toMs int64,
	limit int,
) ([]ports.RangeItem, *problem.Problem) {
	if s.next == nil {
		return nil, nil
	}
	if subject.StreamType == "aggregation.candle" || subject.StreamType == "aggregation.stats" {
		if s.gate != nil && !s.gate.allows(subject.Venue, subject.Symbol, subject.Timeframe) {
			metrics.IncWSQueryRejected("subminute_rollout_blocked")
			return nil, nil
		}
	}
	return s.next.GetRange(ctx, subject, fromMs, toMs, limit)
}

type subMinuteFilteringCandleReader struct {
	next aggports.CandleReader
	gate *subMinuteRolloutGate
}

func (r subMinuteFilteringCandleReader) GetCandleRange(
	ctx context.Context,
	venue, instrument, timeframe string,
	fromMs, toMs int64,
	limit int,
) ([]aggdomain.CandleV1, *problem.Problem) {
	if r.next == nil {
		return nil, nil
	}
	if r.gate != nil && !r.gate.allows(venue, instrument, timeframe) {
		return nil, nil
	}
	return r.next.GetCandleRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
}

func (r subMinuteFilteringCandleReader) GetCandleTimestamps(
	ctx context.Context,
	venue, instrument, timeframe string,
	fromMs, toMs int64,
) ([]int64, *problem.Problem) {
	if r.next == nil {
		return nil, nil
	}
	if r.gate != nil && !r.gate.allows(venue, instrument, timeframe) {
		return nil, nil
	}
	return r.next.GetCandleTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
}

func (r subMinuteFilteringCandleReader) GetFirstCandle(
	ctx context.Context,
	venue, instrument, timeframe string,
) (*aggdomain.CandleV1, *problem.Problem) {
	if r.next == nil {
		return nil, nil
	}
	if r.gate != nil && !r.gate.allows(venue, instrument, timeframe) {
		return nil, nil
	}
	return r.next.GetFirstCandle(ctx, venue, instrument, timeframe)
}

func (r subMinuteFilteringCandleReader) GetLastCandle(
	ctx context.Context,
	venue, instrument, timeframe string,
) (*aggdomain.CandleV1, *problem.Problem) {
	if r.next == nil {
		return nil, nil
	}
	if r.gate != nil && !r.gate.allows(venue, instrument, timeframe) {
		return nil, nil
	}
	return r.next.GetLastCandle(ctx, venue, instrument, timeframe)
}

type subMinuteFilteringStatsReader struct {
	next aggports.StatsReader
	gate *subMinuteRolloutGate
}

func (r subMinuteFilteringStatsReader) GetStatsTimestamps(
	ctx context.Context,
	venue, instrument, timeframe string,
	fromMs, toMs int64,
) ([]int64, *problem.Problem) {
	if r.next == nil {
		return nil, nil
	}
	if r.gate != nil && !r.gate.allows(venue, instrument, timeframe) {
		return nil, nil
	}
	return r.next.GetStatsTimestamps(ctx, venue, instrument, timeframe, fromMs, toMs)
}

func (r subMinuteFilteringStatsReader) GetStatsRange(
	ctx context.Context,
	venue, instrument, timeframe string,
	fromMs, toMs int64,
	limit int,
) ([]aggdomain.StatsWindowV1, *problem.Problem) {
	if r.next == nil {
		return nil, nil
	}
	if r.gate != nil && !r.gate.allows(venue, instrument, timeframe) {
		return nil, nil
	}
	return r.next.GetStatsRange(ctx, venue, instrument, timeframe, fromMs, toMs, limit)
}

func (r subMinuteFilteringStatsReader) GetFirstStats(
	ctx context.Context,
	venue, instrument, timeframe string,
) (*aggdomain.StatsWindowV1, *problem.Problem) {
	if r.next == nil {
		return nil, nil
	}
	if r.gate != nil && !r.gate.allows(venue, instrument, timeframe) {
		return nil, nil
	}
	return r.next.GetFirstStats(ctx, venue, instrument, timeframe)
}

func (r subMinuteFilteringStatsReader) GetLastStats(
	ctx context.Context,
	venue, instrument, timeframe string,
) (*aggdomain.StatsWindowV1, *problem.Problem) {
	if r.next == nil {
		return nil, nil
	}
	if r.gate != nil && !r.gate.allows(venue, instrument, timeframe) {
		return nil, nil
	}
	return r.next.GetLastStats(ctx, venue, instrument, timeframe)
}

// Run is the server composition root.  It wires all dependencies, starts
// the actor engine, HTTP server, and blocks until a signal or fatal error.
//
//nolint:gocyclo // composition root intentionally wires many runtime branches.
func Run(ctx context.Context, cfg config.AppConfig, configPath string) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)
	contracts.SetProtoRolloutConfig(cfg.ProtoRollout.EventTypeFlags())
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		return fmt.Errorf("payload codec registry bootstrap: %v", p)
	}
	logger.Info("server starting", "addr", cfg.HTTP.Addr)
	subMinuteGate := newSubMinuteRolloutGate(cfg.Processor.SubMinuteRollout)
	logger.Info("server: sub-minute rollout gate configured",
		"enabled", subMinuteGate.enabled,
		"venue_allowlist", len(subMinuteGate.venues),
		"instrument_allowlist", len(subMinuteGate.instruments),
	)
	var tsPool *timescale.Pool
	timescale.SetStubMode(timescale.AdapterModeStubMemory)
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
		timescale.SetProductionReady(timescale.AdapterModePGX)
		logger.Info("server: using Timescale pgx pool")
	}

	// ── ClickHouse cold readers ──────────────────────────────────────────
	var coldOpt httpserver.Option
	if cfg.Storage.ClickHouse.Enabled {
		chPool, chP := clickhouse.NewPool(ctx, clickhouse.PoolConfig{
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
		if chP != nil {
			logger.Warn("server: clickhouse pool init failed, cold reader APIs disabled", "err", chP)
		} else {
			defer func() {
				if p := chPool.Close(); p != nil {
					logger.Warn("server: clickhouse pool close failed", "err", p)
				}
			}()
			coldOpt = httpserver.WithColdReaders(&httpserver.ColdReaders{
				Candles: subMinuteFilteringCandleReader{
					next: clickhouse.NewChCandleReader(chPool),
					gate: subMinuteGate,
				},
				Stats: subMinuteFilteringStatsReader{
					next: clickhouse.NewChStatsReader(chPool),
					gate: subMinuteGate,
				},
				Snapshots: clickhouse.NewChSnapshotReader(chPool),
			})
			logger.Info("server: cold reader APIs enabled (ClickHouse)")
		}
	}
	if !timescale.IsProductionReady() {
		logger.Warn("server: timescale adapter running in non-production stub mode",
			"adapter_mode", timescale.AdapterMode(),
		)
	}

	// ── engine ────────────────────────────────────────────────────────────
	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		return err
	}

	// ── delivery wiring ───────────────────────────────────────────────────
	routerPIDCh := make(chan *actor.PID, 1)
	subsystemPIDCh := make(chan *actor.PID, 1)
	var eventBus *bus.InMemoryBus
	var rangeStore deliveryEnvelopeRangeStore
	if tsPool != nil {
		rangeStore = subMinuteFilteringRangeStore{
			next: timescale.NewPgRangeStore(tsPool, 4096),
			gate: subMinuteGate,
		}
		logger.Info("server: using Timescale range store")
	} else {
		rangeStore = subMinuteFilteringRangeStore{
			next: timescale.NewDeliveryRangeStore(4096),
			gate: subMinuteGate,
		}
	}

	// ── JetStream → InMemoryBus bridge ───────────────────────────────────
	var shutdownJSConsumer func(context.Context)

	var deliveryFactory actor.Producer
	if cfg.Delivery.Enabled {
		eventBus = bus.NewInMemoryBus(cfg.Processor.BusCapacity, metrics.NewBusObserver())

		if strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
			jsConsumer, p := adapterjs.NewConsumer(ctx, adapterjs.ConsumerConfig{
				URL:             cfg.JetStream.URL,
				StreamName:      cfg.JetStream.StreamName,
				DedupWindow:     cfg.JetStream.DedupWindowDuration(),
				MaxAge:          cfg.JetStream.MaxAgeDuration(),
				MaxBytes:        cfg.JetStream.MaxBytesInt64(),
				ConsumerDurable: cfg.JetStream.ConsumerDurable,
				FilterSubjects:  cfg.JetStream.FilterSubjects,
				AckWait:         cfg.JetStream.AckWaitDuration(),
				MaxAckPending:   cfg.JetStream.MaxAckPending,
				MaxDeliver:      cfg.JetStream.MaxDeliver,
				DeliverPolicy:   cfg.JetStream.DeliverPolicy,
			}, metrics.NewBusObserver())
			if p != nil {
				return fmt.Errorf("server: jetstream consumer init failed: %v", p)
			}
			consumeCtx, cancelConsume := context.WithCancel(ctx)
			go func() {
				if p := jsConsumer.Consume(consumeCtx, func(_ context.Context, env envelope.Envelope) *problem.Problem {
					rangeStore.StoreEnvelope(env)
					return eventBus.Publish(ctx, env)
				}); p != nil {
					logger.Error("server: jetstream consume loop failed", "err", p)
				}
			}()
			shutdownJSConsumer = func(shutCtx context.Context) {
				cancelConsume()
				if p := jsConsumer.Close(shutCtx); p != nil {
					logger.Warn("server: jetstream consumer close failed", "err", p)
				}
			}
			logger.Info("server: subscribed to jetstream consumer",
				"url", cfg.JetStream.URL,
				"stream", cfg.JetStream.StreamName,
				"durable", cfg.JetStream.ConsumerDurable,
				"filters", cfg.JetStream.FilterSubjects,
			)
		}

		deliveryCfg := deliveryruntime.SubsystemConfig{
			Logger:       logger,
			EnvelopeCh:   eventBus.Subscribe(),
			MaxSessions:  cfg.Delivery.MaxSessions,
			Backpressure: cfg.Delivery.BackpressurePolicy,
			NATSDurable:  cfg.Delivery.NATS.ConsumerDurable,
			NATSSubjects: append([]string(nil), cfg.Delivery.NATS.FilterSubjects...),
			Router: deliveryruntime.RouterConfig{
				Logger:              logger,
				Timeframe:           "raw",
				EnvelopeStore:       rangeStore,
				StreamCoherenceMode: "sticky_session",
				StreamStateTTL:      cfg.Delivery.RouterStreamStateTTLDuration(),
			},
			OnRouterReady: func(pid *actor.PID) {
				select {
				case routerPIDCh <- pid:
				default:
				}
			},
			OnReady: func(subsystemPID, _ *actor.PID) {
				select {
				case subsystemPIDCh <- subsystemPID:
				default:
				}
			},
		}
		deliveryFactory = deliveryruntime.NewSubsystemActor(deliveryCfg)
	}

	// ── evidence engine ──────────────────────────────────────────────────
	var evidenceFactory actor.Producer
	var signalFactory actor.Producer
	if cfg.Delivery.Enabled {
		ruleCfg := evidenceapp.DefaultRuleConfig()
		engineCfg := evidenceapp.DefaultEngineConfig()
		engineCfg.BufferCapPerKind = cfg.Evidence.BufferCapPerKind
		engineCfg.DecayHalfLife = time.Duration(cfg.Evidence.DecayHalfLifeMs) * time.Millisecond
		regimePolicy, p := evidencedomain.NewRegimeStorePolicy(cfg.Evidence.RegimeMaxStreams, cfg.Evidence.RegimeHistoryCap)
		if p != nil {
			return p
		}
		evidenceEngine := evidenceapp.NewEvidenceEngine(engineCfg,
			evidenceapp.NewSpreadExplosionRule(ruleCfg),
			evidenceapp.NewLiquidityThinningRule(ruleCfg),
			evidenceapp.NewPersistentImbalanceRule(ruleCfg),
			evidenceapp.NewAbsorptionRule(ruleCfg),
			evidenceapp.NewSweepRule(ruleCfg),
		)
		lelEngine := evidenceapp.NewLELEngine(
			evidenceapp.DefaultLELEngineConfig(),
			evidenceapp.NewLELBookImbalanceRule(ruleCfg),
			evidenceapp.NewLELAbsorptionRule(ruleCfg),
			evidenceapp.NewLELSweepRule(ruleCfg),
			evidenceapp.NewLELThinningRule(ruleCfg),
			evidenceapp.NewLELSpreadRegimeRule(ruleCfg),
		)
		evidenceFactory = evidenceruntime.NewSubsystemActor(evidenceruntime.SubsystemConfig{
			Logger:      logger.With("subsystem", "evidence"),
			EnvelopeCh:  eventBus.Subscribe(),
			Engine:      evidenceEngine,
			LELEngine:   lelEngine,
			RegimeStore: evidencedomain.NewRegimeStore(regimePolicy),
			RegimeDetectors: []evidenceapp.RegimeDetector{
				evidenceapp.NewBreakoutRegimeDetector(evidenceapp.DefaultBreakoutPolicy()),
				evidenceapp.NewTrendRegimeDetector(evidenceapp.DefaultTrendPolicy()),
				evidenceapp.NewVolatilityRegimeDetector(evidenceapp.DefaultVolatilityPolicy()),
			},
			Publisher:    eventBus,
			ReplicaID:    cfg.Shard.Index,
			ReplicaCount: cfg.Shard.Count,
		})
		if cfg.Signals.UseComposer {
			composePolicy := signalsapp.DefaultComposePolicy()
			composePolicy.CorrelationWindowMs = cfg.Signals.CorrelationWindowMs
			limiterPolicy := signalsapp.DefaultRateLimitPolicy()
			limiterPolicy.DedupWindowMs = cfg.Signals.DedupWindowMs
			limiterPolicy.DedupCapPerKey = cfg.Signals.WindowCap
			limiterPolicy.RateLimitPerMin = cfg.Signals.RateLimitPerMin
			limiterPolicy.GlobalRateLimitMin = cfg.Signals.GlobalRateLimitPerMin

			signalFactory = signalsruntime.NewSubsystemActor(signalsruntime.SubsystemConfig{
				Logger:                logger.With("subsystem", "signals"),
				EnvelopeCh:            eventBus.Subscribe(),
				Composer:              signalsapp.NewSignalComposer(composePolicy),
				Limiter:               signalsapp.NewSignalRateLimiter(limiterPolicy),
				RegimeCacheMaxStreams: cfg.Evidence.RegimeMaxStreams,
				Publisher:             eventBus,
				ReplicaID:             cfg.Shard.Index,
				ReplicaCount:          cfg.Shard.Count,
			})
		}
	}

	// ── guardian ──────────────────────────────────────────────────────────
	guardianPID := actorruntime.SpawnGuardian(e, actorruntime.GuardianConfig{
		Logger:    logger,
		Factories: buildServerFactories(cfg.Delivery.Enabled, deliveryFactory, evidenceFactory, signalFactory),
	})
	logger.Info("guardian spawned", "pid", guardianPID.String())

	// ── HTTP server ──────────────────────────────────────────────────────
	var marketsOpt httpserver.Option
	if len(cfg.Markets.Exchanges) > 0 {
		marketsOpt = httpserver.WithMarkets(&cfg.Markets)
		logger.Info("server: markets discovery enabled", "exchanges", len(cfg.Markets.Exchanges))
	}
	srv := httpserver.NewServer(
		e,
		guardianPID,
		cfg.HTTP.Addr,
		cfg.HTTP.EnablePprof,
		logger,
		httpserver.WithTLS(cfg.HTTP.TLSCert, cfg.HTTP.TLSKey),
		httpserver.WithReloadHook(protoRolloutReloadHook(configPath, logger)),
		coldOpt,
		marketsOpt,
	)
	if cfg.Delivery.Enabled {
		enableWSRoute(e, srv, routerPIDCh, subsystemPIDCh, logger, rangeStore, tsPool, subMinuteGate, cfg)
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// ── wait for signal or error ─────────────────────────────────────────
	quit := bootstrap.SignalChannel()
	select {
	case sig := <-quit:
		logger.Info("server: received signal", "signal", sig)
	case err := <-serverErr:
		logger.Error("server: HTTP server error", "err", err)
	case <-ctx.Done():
		logger.Info("server: context canceled")
	}

	// ── shutdown ─────────────────────────────────────────────────────────
	logger.Info("server: shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())

	if shutdownJSConsumer != nil {
		shutdownJSConsumer(shutCtx)
	}
	if eventBus != nil {
		eventBus.Close()
	}
	defer cancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Warn("server: HTTP shutdown error", "err", err)
	}

	actorruntime.ShutdownGuardian(shutCtx, e, guardianPID, logger)
	logger.Info("server: shutdown complete")
	return nil
}

func int32FromConfig(v int, field string) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, fmt.Errorf("%s out of int32 range: %d", field, v)
	}
	return int32(v), nil
}

func enableWSRoute(
	e *actor.Engine,
	srv *httpserver.Server,
	routerPIDCh <-chan *actor.PID,
	subsystemPIDCh <-chan *actor.PID,
	logger *slog.Logger,
	rangeStore ports.RangeStore,
	tsPool *timescale.Pool,
	subMinuteGate *subMinuteRolloutGate,
	cfg config.AppConfig,
) {
	hotSnapshotProvider := newWSHotSnapshotProvider(rangeStore, tsPool, subMinuteGate)
	hotSnapshotProvider = newBoundedSnapshotCacheProvider(hotSnapshotProvider, wsSnapshotCacheTTL, wsSnapshotCacheMaxEntries)
	metrics.ConfigureWSTenantLabelPolicy(
		cfg.WS.TenantMetrics.IncludeTenantLabel,
		cfg.WS.TenantMetrics.TenantWhitelist,
		cfg.WS.TenantMetrics.Fallback,
	)
	select {
	case routerPID := <-routerPIDCh:
		var subsystemPID *actor.PID
		select {
		case subsystemPID = <-subsystemPIDCh:
		case <-time.After(cfg.Delivery.SubsystemReadyTimeoutDuration()):
		}
		ws := wsserver.NewServer(
			e,
			routerPID,
			logger,
			rangeStore,
			cfg.Delivery.SessionOutboundQueueSize,
			wsserver.WithAuthConfig(wsserver.AuthConfig{
				Enabled:      cfg.WS.Auth.Enabled,
				APIKeys:      cfg.WS.Auth.APIKeys,
				APIKeyScopes: cfg.WS.Auth.APIKeyScopes,
				JWT: wsserver.JWTAuthConfig{
					Enabled:     cfg.WS.Auth.JWT.Enabled,
					HS256Secret: cfg.WS.Auth.JWT.HS256Secret,
					Issuer:      cfg.WS.Auth.JWT.Issuer,
					Audience:    cfg.WS.Auth.JWT.Audience,
				},
			}),
			wsserver.WithSessionSpawner(func(sessionCfg deliveryruntime.SessionConfig) *actor.PID {
				sessionCfg.BackpressurePolicy = deliverydomain.BackpressurePolicy(cfg.Delivery.BackpressurePolicy)
				sessionCfg.HotSnapshotProvider = hotSnapshotProvider
				sessionCfg.MetricsCadence = time.Duration(cfg.Delivery.MetricsCadenceMs) * time.Millisecond
				sessionCfg.KeepaliveInterval = time.Duration(cfg.Delivery.KeepaliveIntervalMs) * time.Millisecond
				if sessionCfg.MaxFrameBytes <= 0 {
					sessionCfg.MaxFrameBytes = cfg.Delivery.MaxFrameBytes
				}
				if subsystemPID == nil {
					return nil
				}
				resp := e.Request(subsystemPID, deliveryruntime.SpawnSession{Config: sessionCfg}, cfg.Delivery.SessionSpawnTimeoutDuration())
				result, err := resp.Result()
				if err != nil {
					logger.Warn("delivery session spawn request failed", "err", err)
					return nil
				}
				ack, ok := result.(deliveryruntime.SpawnSessionAck)
				if !ok {
					logger.Warn("delivery session spawn response type mismatch", "type", fmt.Sprintf("%T", result))
					return nil
				}
				return ack.PID
			}),
			wsserver.WithRateLimit(deliveryruntime.RateLimitConfig{
				Enabled:       cfg.WS.RateLimit.Enabled,
				MaxPerSecond:  cfg.WS.RateLimit.MaxPerSecond,
				BurstCapacity: cfg.WS.RateLimit.BurstCapacity,
			}),
			wsserver.WithSignalSubscriptionLimit(cfg.Signals.MaxSubsPerSession),
			wsserver.WithIPRateLimit(deliveryruntime.RateLimitConfig{
				Enabled:       cfg.WS.RateLimit.Enabled,
				MaxPerSecond:  cfg.WS.RateLimit.MaxPerSecond,
				BurstCapacity: cfg.WS.RateLimit.BurstCapacity,
			}),
			wsserver.WithConnectionLimits(wsserver.ServerConnectionLimits{
				MaxConnectionsPerIP:  cfg.WS.Limits.MaxConnectionsPerIP,
				MaxConnectionsPerKey: cfg.WS.Limits.MaxConnectionsPerKey,
				MaxSubsPerConnection: cfg.WS.Limits.MaxSubsPerConnection,
				MaxSymbolsPerConn:    cfg.WS.Limits.MaxSymbolsPerConn,
			}),
			wsserver.WithTenantLimits(cfg.WS.TenantLimits),
			wsserver.WithServerInstanceID(ids.NewSessionID().String()),
			wsserver.WithSlowClientDropThreshold(cfg.Delivery.SlowClientDropThreshold),
			wsserver.WithTranscodeCache(deliveryruntime.NewTranscodeCache(0)),
			wsserver.WithMaxFrameBytes(cfg.Delivery.MaxFrameBytes),
		)
		srv.HandleFunc("GET /ws", ws.HandleWS)
		srv.HandleFunc("GET /introspection", ws.HandleIntrospection)
		logger.Info("delivery websocket route enabled", "route", "GET /ws (v1)")
	case <-time.After(cfg.Delivery.RouterReadyTimeoutDuration()):
		logger.Warn("delivery router not ready in time; /ws route disabled")
	}
}

const wsHotSnapshotRecentWindow = 24 * time.Hour
const wsSnapshotCacheTTL = 3 * time.Second
const wsSnapshotCacheMaxEntries = 1024

type rangeStoreHotSnapshotProvider struct {
	rangeStore ports.RangeStore
}

func newRangeStoreHotSnapshotProvider(rangeStore ports.RangeStore) deliveryruntime.HotSnapshotProvider {
	if rangeStore == nil {
		return nil
	}
	return rangeStoreHotSnapshotProvider{rangeStore: rangeStore}
}

type wsHotSnapshotProvider struct {
	live  deliveryruntime.HotSnapshotProvider
	hotTS deliveryruntime.HotSnapshotProvider
}

type snapshotCacheEntry struct {
	payload  []byte
	cachedAt time.Time
}

type boundedSnapshotCacheProvider struct {
	next       deliveryruntime.HotSnapshotProvider
	ttl        time.Duration
	maxEntries int
	clock      func() time.Time

	mu    sync.Mutex
	cache map[string]snapshotCacheEntry
	order []string
}

func newWSHotSnapshotProvider(
	rangeStore ports.RangeStore,
	tsPool *timescale.Pool,
	subMinuteGate *subMinuteRolloutGate,
) deliveryruntime.HotSnapshotProvider {
	p := wsHotSnapshotProvider{
		live:  newRangeStoreHotSnapshotProvider(rangeStore),
		hotTS: newTimescaleAggregateHotSnapshotProvider(tsPool, subMinuteGate),
	}
	if p.live == nil && p.hotTS == nil {
		return nil
	}
	return p
}

func newBoundedSnapshotCacheProvider(
	next deliveryruntime.HotSnapshotProvider,
	ttl time.Duration,
	maxEntries int,
) deliveryruntime.HotSnapshotProvider {
	if next == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = wsSnapshotCacheTTL
	}
	if maxEntries <= 0 {
		maxEntries = wsSnapshotCacheMaxEntries
	}
	provider := &boundedSnapshotCacheProvider{
		next:       next,
		ttl:        ttl,
		maxEntries: maxEntries,
		clock:      time.Now,
		cache:      make(map[string]snapshotCacheEntry, maxEntries),
		order:      make([]string, 0, maxEntries),
	}
	metrics.SetDeliveryWSSnapshotCacheEntries(0)
	return provider
}

func (p wsHotSnapshotProvider) GetLatest(subject deliverydomain.Subject) ([]byte, bool) {
	if p.live != nil {
		if raw, ok := p.live.GetLatest(subject); ok {
			return raw, true
		}
	}
	if p.hotTS != nil {
		return p.hotTS.GetLatest(subject)
	}
	return nil, false
}

func (p *boundedSnapshotCacheProvider) GetLatest(subject deliverydomain.Subject) ([]byte, bool) {
	if p == nil || p.next == nil {
		return nil, false
	}
	key := strings.TrimSpace(subject.String())
	if key == "" {
		return p.next.GetLatest(subject)
	}
	now := time.Now()
	if p.clock != nil {
		now = p.clock()
	}

	p.mu.Lock()
	if entry, ok := p.cache[key]; ok {
		if now.Sub(entry.cachedAt) <= p.ttl {
			payload := append([]byte(nil), entry.payload...)
			p.mu.Unlock()
			metrics.IncDeliveryWSSnapshotCacheHit()
			return payload, true
		}
		delete(p.cache, key)
		p.removeOrderKey(key)
		metrics.SetDeliveryWSSnapshotCacheEntries(len(p.cache))
	}
	p.mu.Unlock()

	metrics.IncDeliveryWSSnapshotCacheMiss()
	payload, ok := p.next.GetLatest(subject)
	if !ok || len(payload) == 0 {
		return payload, ok
	}
	copied := append([]byte(nil), payload...)
	p.mu.Lock()
	if _, exists := p.cache[key]; !exists {
		p.order = append(p.order, key)
	}
	p.cache[key] = snapshotCacheEntry{payload: copied, cachedAt: now}
	for len(p.cache) > p.maxEntries && len(p.order) > 0 {
		evict := p.order[0]
		p.order = p.order[1:]
		if evict == key {
			continue
		}
		delete(p.cache, evict)
	}
	metrics.SetDeliveryWSSnapshotCacheEntries(len(p.cache))
	p.mu.Unlock()
	return copied, true
}

func (p *boundedSnapshotCacheProvider) removeOrderKey(key string) {
	if p == nil || len(p.order) == 0 || key == "" {
		return
	}
	dst := p.order[:0]
	for _, current := range p.order {
		if current != key {
			dst = append(dst, current)
		}
	}
	p.order = dst
}

func (p rangeStoreHotSnapshotProvider) GetLatest(subject deliverydomain.Subject) ([]byte, bool) {
	if p.rangeStore == nil {
		return nil, false
	}
	// Keep this focused on low-frequency aggregated streams to avoid expensive
	// scans or stale results from capped range queries on high-frequency streams.
	switch subject.StreamType {
	case "aggregation.candle", "aggregation.stats":
	default:
		return nil, false
	}

	if payload, ok := p.getLatestForSubject(subject); ok {
		return payload, true
	}
	// Compatibility: range store is keyed by canonical envelope instrument (no
	// :MARKET_TYPE suffix) while WS clients may subscribe to alias subjects.
	if i := strings.IndexByte(subject.Symbol, ':'); i > 0 {
		fallback := subject
		fallback.Symbol = subject.Symbol[:i]
		return p.getLatestForSubject(fallback)
	}
	return nil, false
}

func (p rangeStoreHotSnapshotProvider) getLatestForSubject(subject deliverydomain.Subject) ([]byte, bool) {
	nowMs := time.Now().UnixMilli()
	fromMs := nowMs - wsHotSnapshotRecentWindow.Milliseconds()
	items, prob := p.rangeStore.GetRange(context.Background(), subject, fromMs, 0, 4096)
	if prob != nil || len(items) == 0 {
		return nil, false
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].TsIngest == items[j].TsIngest {
			return items[i].Seq < items[j].Seq
		}
		return items[i].TsIngest < items[j].TsIngest
	})
	payload := items[len(items)-1].Payload
	if len(payload) == 0 {
		return nil, false
	}
	return append([]byte(nil), payload...), true
}

type timescaleAggregateHotSnapshotProvider struct {
	pool *timescale.Pool
	gate *subMinuteRolloutGate
}

func newTimescaleAggregateHotSnapshotProvider(
	pool *timescale.Pool,
	gate *subMinuteRolloutGate,
) deliveryruntime.HotSnapshotProvider {
	if pool == nil || pool.Raw() == nil {
		return nil
	}
	return timescaleAggregateHotSnapshotProvider{pool: pool, gate: gate}
}

func (p timescaleAggregateHotSnapshotProvider) GetLatest(subject deliverydomain.Subject) ([]byte, bool) {
	if p.pool == nil || p.pool.Raw() == nil {
		return nil, false
	}
	switch subject.StreamType {
	case "aggregation.candle":
		return p.getLatestCandle(subject)
	case "aggregation.stats":
		return p.getLatestStats(subject)
	default:
		return nil, false
	}
}

func (p timescaleAggregateHotSnapshotProvider) getLatestCandle(subject deliverydomain.Subject) ([]byte, bool) {
	tf := strings.TrimSpace(subject.Timeframe)
	if tf == "" || strings.EqualFold(tf, "raw") {
		tf = "1m"
	}
	if p.gate != nil && !p.gate.allows(subject.Venue, subject.Symbol, tf) {
		return nil, false
	}
	venue := strings.ToUpper(strings.TrimSpace(subject.Venue))
	symbols := snapshotSymbolCandidates(subject.Symbol)
	for _, instrument := range symbols {
		var c aggdomain.CandleV1
		err := p.pool.Raw().QueryRow(context.Background(), `
SELECT venue, instrument, timeframe, window_start, window_end,
       open_price, high_price, low_price, close_price,
       volume, buy_volume, sell_volume, trade_count, seq_first, seq_last
FROM aggregation_candle
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
ORDER BY window_end DESC
LIMIT 1
`, venue, instrument, tf).Scan(
			&c.Venue,
			&c.Instrument,
			&c.Timeframe,
			&c.WindowStartTs,
			&c.WindowEndTs,
			&c.Open,
			&c.High,
			&c.Low,
			&c.ClosePrice,
			&c.Volume,
			&c.BuyVolume,
			&c.SellVolume,
			&c.TradeCount,
			&c.SeqFirst,
			&c.SeqLast,
		)
		if err != nil {
			continue
		}
		c.IsClosed = true
		raw, err := json.Marshal(c)
		if err != nil || len(raw) == 0 {
			return nil, false
		}
		return raw, true
	}
	return nil, false
}

func (p timescaleAggregateHotSnapshotProvider) getLatestStats(subject deliverydomain.Subject) ([]byte, bool) {
	tf := strings.TrimSpace(subject.Timeframe)
	if tf == "" || strings.EqualFold(tf, "raw") {
		tf = "1m"
	}
	if p.gate != nil && !p.gate.allows(subject.Venue, subject.Symbol, tf) {
		return nil, false
	}
	venue := strings.ToUpper(strings.TrimSpace(subject.Venue))
	symbols := snapshotSymbolCandidates(subject.Symbol)
	for _, instrument := range symbols {
		var (
			s               aggdomain.StatsWindowV1
			markPriceOpen   sql.NullFloat64
			markPriceHigh   sql.NullFloat64
			markPriceLow    sql.NullFloat64
			markPriceClose  sql.NullFloat64
			fundingRateAvg  sql.NullFloat64
			fundingRateLast sql.NullFloat64
		)
		err := p.pool.Raw().QueryRow(context.Background(), `
SELECT venue, instrument, timeframe, window_start, window_end,
       liq_buy_volume, liq_sell_volume, liq_total_volume, liq_count,
       markprice_open, markprice_high, markprice_low, markprice_close,
       funding_rate_avg, funding_rate_last, seq_first, seq_last
FROM aggregation_stats
WHERE venue = $1 AND instrument = $2 AND timeframe = $3
ORDER BY window_end DESC
LIMIT 1
`, venue, instrument, tf).Scan(
			&s.Venue,
			&s.Instrument,
			&s.Timeframe,
			&s.WindowStartTs,
			&s.WindowEndTs,
			&s.LiqBuyVolume,
			&s.LiqSellVolume,
			&s.LiqTotalVolume,
			&s.LiqCount,
			&markPriceOpen,
			&markPriceHigh,
			&markPriceLow,
			&markPriceClose,
			&fundingRateAvg,
			&fundingRateLast,
			&s.SeqFirst,
			&s.SeqLast,
		)
		if err != nil {
			continue
		}
		s.MarkPriceOpen = markPriceOpen.Float64
		s.MarkPriceHigh = markPriceHigh.Float64
		s.MarkPriceLow = markPriceLow.Float64
		s.MarkPriceClose = markPriceClose.Float64
		s.FundingRateAvg = fundingRateAvg.Float64
		s.FundingRateLast = fundingRateLast.Float64
		s.IsClosed = true
		raw, err := json.Marshal(s)
		if err != nil || len(raw) == 0 {
			return nil, false
		}
		return raw, true
	}
	return nil, false
}

func snapshotSymbolCandidates(symbol string) []string {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil
	}
	out := []string{strings.ToUpper(symbol)}
	if i := strings.IndexByte(symbol, ':'); i > 0 {
		out = append(out, strings.ToUpper(symbol[:i]))
	}
	return out
}

func buildServerFactories(deliveryEnabled bool, deliveryFactory, evidenceFactory, signalFactory actor.Producer) map[actorruntime.Subsystem]actor.Producer {
	factories := make(map[actorruntime.Subsystem]actor.Producer)
	if deliveryEnabled && deliveryFactory != nil {
		factories[actorruntime.SubsystemDelivery] = deliveryFactory
	}
	if deliveryEnabled && evidenceFactory != nil {
		factories[actorruntime.SubsystemEvidence] = evidenceFactory
	}
	if deliveryEnabled && signalFactory != nil {
		factories[actorruntime.SubsystemSignals] = signalFactory
	}
	return factories
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
		logger.Info("server: proto rollout flags reloaded", "config", configPath)
		return nil
	}
}
