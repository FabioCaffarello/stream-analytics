package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/anthdm/hollywood/actor"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	"github.com/market-raccoon/internal/adapters/storage/timescale"
	"github.com/market-raccoon/internal/core/delivery/ports"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	wsserver "github.com/market-raccoon/internal/interfaces/ws"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
)

// Run is the server composition root.  It wires all dependencies, starts
// the actor engine, HTTP server, and blocks until a signal or fatal error.
func Run(ctx context.Context, cfg config.AppConfig) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)
	logger.Info("server starting", "addr", cfg.HTTP.Addr)
	var tsPool *timescale.Pool
	timescale.SetStubMode(timescale.AdapterModeStubMemory)
	if cfg.Storage.Timescale.Enabled {
		pool, p := timescale.NewPool(ctx, timescale.PoolConfig{
			DSN:               cfg.Storage.Timescale.DSN,
			MaxConns:          int32(cfg.Storage.Timescale.MaxConns),
			MinConns:          int32(cfg.Storage.Timescale.MinConns),
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
	eventBus := bus.NewInMemoryBus(cfg.Processor.BusCapacity, metrics.NewBusObserver())
	envelopeCh := eventBus.Subscribe()
	routerPIDCh := make(chan *actor.PID, 1)
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

	deliveryCfg := deliveryruntime.SubsystemConfig{
		Logger: logger,
		Router: deliveryruntime.RouterConfig{
			Logger:        logger,
			EnvelopeCh:    envelopeCh,
			Timeframe:     "raw",
			EnvelopeStore: rangeStore,
		},
		OnRouterReady: func(pid *actor.PID) {
			select {
			case routerPIDCh <- pid:
			default:
			}
		},
	}

	// ── guardian ──────────────────────────────────────────────────────────
	guardianPID := actorruntime.SpawnGuardian(e, actorruntime.GuardianConfig{
		Logger: logger,
		Factories: map[actorruntime.Subsystem]actor.Producer{
			actorruntime.SubsystemDelivery: deliveryruntime.NewSubsystemActor(deliveryCfg),
		},
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
	)
	enableWSRoute(e, srv, routerPIDCh, logger, rangeStore, cfg)

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
	eventBus.Close()

	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer cancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Warn("server: HTTP shutdown error", "err", err)
	}

	actorruntime.ShutdownGuardian(shutCtx, e, guardianPID, logger)
	logger.Info("server: shutdown complete")
	return nil
}

func enableWSRoute(
	e *actor.Engine,
	srv *httpserver.Server,
	routerPIDCh <-chan *actor.PID,
	logger *slog.Logger,
	rangeStore ports.RangeStore,
	cfg config.AppConfig,
) {
	select {
	case routerPID := <-routerPIDCh:
		ws := wsserver.NewServer(
			e,
			routerPID,
			logger,
			rangeStore,
			256,
			wsserver.WithAuthConfig(wsserver.AuthConfig{
				Enabled: cfg.WS.Auth.Enabled,
				APIKeys: cfg.WS.Auth.APIKeys,
			}),
			wsserver.WithRateLimit(deliveryruntime.RateLimitConfig{
				Enabled:       cfg.WS.RateLimit.Enabled,
				MaxPerSecond:  cfg.WS.RateLimit.MaxPerSecond,
				BurstCapacity: cfg.WS.RateLimit.BurstCapacity,
			}),
		)
		srv.HandleFunc("GET /ws", ws.HandleWS)
		logger.Info("delivery websocket route enabled", "route", "GET /ws")
	case <-time.After(2 * time.Second):
		logger.Warn("delivery router not ready in time; /ws route disabled")
	}
}
