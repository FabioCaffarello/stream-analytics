// Package main is the market-raccoon server binary.
//
// The server exposes runtime observability and control over HTTP.  It does NOT
// ingest market data or run any business logic — it only supervises the actor
// engine and proxies requests to the Guardian.
//
// v1 wiring:
//
//	engine
//	  └─ Guardian  (observer mode — no real subsystem factories)
//	HTTP (net/http)
//	  GET  /healthz           → 200 ok                (liveness)
//	  GET  /readyz            → 200/503 ready state   (readiness)
//	  GET  /runtime/snapshot  → JSON guardian state
//	  POST /runtime/reload    → 202 accepted
//
// Usage:
//
//	go run ./cmd/server [flags]
//	  -config     string   path to JSONC config file (default "config.jsonc")
//	  -addr       string   HTTP listen address override (default from config)
//	  -log-level  string   log level override: debug|info|warn|error
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/anthdm/hollywood/actor"
	deliveryruntime "github.com/market-raccoon/internal/actors/delivery/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	wsserver "github.com/market-raccoon/internal/interfaces/ws"
	"github.com/market-raccoon/internal/shared/config"
)

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	addrOverride := flag.String("addr", "", "HTTP listen address (overrides config)")
	logLevelOverride := flag.String("log-level", "", "log level override: debug|info|warn|error")
	flag.Parse()

	// ── config ────────────────────────────────────────────────────────────────
	cfg, prob := config.Load(*configPath)
	if prob != nil {
		slog.Error("server: config load failed", "err", prob)
		os.Exit(1)
	}
	// Apply CLI overrides before validation.
	if *addrOverride != "" {
		cfg.HTTP.Addr = *addrOverride
	}
	if *logLevelOverride != "" {
		cfg.Log.Level = *logLevelOverride
	}
	if prob = cfg.Validate(); prob != nil {
		slog.Error("server: config validation failed", "err", prob)
		os.Exit(1)
	}

	// ── logger ────────────────────────────────────────────────────────────────
	logger := buildLogger(cfg.Log)
	slog.SetDefault(logger)

	logger.Info("server starting", "addr", cfg.HTTP.Addr)

	// ── engine ────────────────────────────────────────────────────────────────
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		logger.Error("failed to create actor engine", "err", err)
		os.Exit(1)
	}

	// ── delivery wiring (W2, in-memory source) ───────────────────────────────
	eventBus := bus.NewInMemoryBus(cfg.Processor.BusCapacity)
	envelopeCh := eventBus.Subscribe()
	routerPIDCh := make(chan *actor.PID, 1)

	deliveryCfg := deliveryruntime.SubsystemConfig{
		Logger: logger,
		Router: deliveryruntime.RouterConfig{
			Logger:     logger,
			EnvelopeCh: envelopeCh,
			Timeframe:  "raw",
		},
		OnRouterReady: func(pid *actor.PID) {
			select {
			case routerPIDCh <- pid:
			default:
			}
		},
	}

	// ── guardian ──────────────────────────────────────────────────────────────
	guardianPID := e.Spawn(
		actorruntime.NewGuardian(actorruntime.GuardianConfig{
			Logger: logger,
			Factories: map[actorruntime.Subsystem]actor.Producer{
				actorruntime.SubsystemDelivery: deliveryruntime.NewSubsystemActor(deliveryCfg),
			},
		}),
		"guardian",
		actor.WithID("guardian"),
	)
	logger.Info("guardian spawned", "pid", guardianPID.String())

	// ── HTTP server ───────────────────────────────────────────────────────────
	srv := httpserver.NewServer(e, guardianPID, cfg.HTTP.Addr, logger)
	select {
	case routerPID := <-routerPIDCh:
		ws := wsserver.NewServer(e, routerPID, logger)
		srv.HandleFunc("GET /ws", ws.HandleWS)
		logger.Info("delivery websocket route enabled", "route", "GET /ws")
	case <-time.After(2 * time.Second):
		logger.Warn("delivery router not ready in time; /ws route disabled")
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// ── signal handling ───────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		logger.Info("server: received signal", "signal", sig)
	case err := <-serverErr:
		logger.Error("server: HTTP server error", "err", err)
	}

	logger.Info("server: shutting down")
	eventBus.Close()

	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer cancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Warn("server: HTTP shutdown error", "err", err)
	}

	e.Send(guardianPID, actorruntime.Stop{})
	select {
	case <-e.Poison(guardianPID).Done():
	case <-shutCtx.Done():
		logger.Warn("server: guardian did not stop in time")
	}
	logger.Info("server: shutdown complete")
}

func buildLogger(cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(cfg.Level))); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if strings.ToLower(cfg.Format) == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
