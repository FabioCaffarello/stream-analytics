package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/anthdm/hollywood/actor"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	"github.com/market-raccoon/internal/adapters/storage/timescale"
	deliverydomain "github.com/market-raccoon/internal/core/delivery/domain"
	"github.com/market-raccoon/internal/core/delivery/ports"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	wsserver "github.com/market-raccoon/internal/interfaces/ws"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

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
				Candles:   clickhouse.NewChCandleReader(chPool),
				Stats:     clickhouse.NewChStatsReader(chPool),
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
	var rangeStore interface {
		ports.RangeStore
		StoreEnvelope(env envelope.Envelope)
	}
	if tsPool != nil {
		rangeStore = timescale.NewPgRangeStore(tsPool, 4096)
		logger.Info("server: using Timescale range store")
	} else {
		rangeStore = timescale.NewDeliveryRangeStore(4096)
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
				Logger:        logger,
				Timeframe:     "raw",
				EnvelopeStore: rangeStore,
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

	// ── guardian ──────────────────────────────────────────────────────────
	guardianPID := actorruntime.SpawnGuardian(e, actorruntime.GuardianConfig{
		Logger:    logger,
		Factories: buildServerFactories(cfg.Delivery.Enabled, deliveryFactory),
	})
	logger.Info("guardian spawned", "pid", guardianPID.String())

	// ── HTTP server ──────────────────────────────────────────────────────
	srv := httpserver.NewServer(
		e,
		guardianPID,
		cfg.HTTP.Addr,
		cfg.HTTP.EnablePprof,
		logger,
		httpserver.WithTLS(cfg.HTTP.TLSCert, cfg.HTTP.TLSKey),
		httpserver.WithReloadHook(protoRolloutReloadHook(configPath, logger)),
		coldOpt,
	)
	if cfg.Delivery.Enabled {
		enableWSRoute(e, srv, routerPIDCh, subsystemPIDCh, logger, rangeStore, cfg)
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
	cfg config.AppConfig,
) {
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
				Enabled: cfg.WS.Auth.Enabled,
				APIKeys: cfg.WS.Auth.APIKeys,
			}),
			wsserver.WithSessionSpawner(func(sessionCfg deliveryruntime.SessionConfig) *actor.PID {
				sessionCfg.BackpressurePolicy = deliverydomain.BackpressurePolicy(cfg.Delivery.BackpressurePolicy)
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
			wsserver.WithSlowClientDropThreshold(cfg.Delivery.SlowClientDropThreshold),
			wsserver.WithTranscodeCache(deliveryruntime.NewTranscodeCache(0)),
		)
		srv.HandleFunc("GET /ws", ws.HandleWS)
		logger.Info("delivery websocket route enabled", "route", "GET /ws")
	case <-time.After(cfg.Delivery.RouterReadyTimeoutDuration()):
		logger.Warn("delivery router not ready in time; /ws route disabled")
	}
}

func buildServerFactories(deliveryEnabled bool, deliveryFactory actor.Producer) map[actorruntime.Subsystem]actor.Producer {
	factories := make(map[actorruntime.Subsystem]actor.Producer)
	if deliveryEnabled && deliveryFactory != nil {
		factories[actorruntime.SubsystemDelivery] = deliveryFactory
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
