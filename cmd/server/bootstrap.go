package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/anthdm/hollywood/actor"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	"github.com/market-raccoon/internal/adapters/storage/timescale"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	wsserver "github.com/market-raccoon/internal/interfaces/ws"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/metrics"
)

// Run is the server composition root.  It wires all dependencies, starts
// the actor engine, HTTP server, and blocks until a signal or fatal error.
func Run(ctx context.Context, cfg config.AppConfig) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)
	logger.Info("server starting", "addr", cfg.HTTP.Addr)

	// ── engine ────────────────────────────────────────────────────────────
	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		return err
	}

	// ── delivery wiring ───────────────────────────────────────────────────
	eventBus := bus.NewInMemoryBus(cfg.Processor.BusCapacity, metrics.NewBusObserver())
	envelopeCh := eventBus.Subscribe()
	routerPIDCh := make(chan *actor.PID, 1)
	rangeStore := timescale.NewDeliveryRangeStore(4096)

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
	srv := httpserver.NewServer(e, guardianPID, cfg.HTTP.Addr, cfg.HTTP.EnablePprof, logger)
	enableWSRoute(e, srv, routerPIDCh, logger, rangeStore)

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

func enableWSRoute(e *actor.Engine, srv *httpserver.Server, routerPIDCh <-chan *actor.PID, logger *slog.Logger, rangeStore *timescale.DeliveryRangeStore) {
	select {
	case routerPID := <-routerPIDCh:
		ws := wsserver.NewServer(e, routerPID, logger, rangeStore, 256)
		srv.HandleFunc("GET /ws", ws.HandleWS)
		logger.Info("delivery websocket route enabled", "route", "GET /ws")
	case <-time.After(2 * time.Second):
		logger.Warn("delivery router not ready in time; /ws route disabled")
	}
}
