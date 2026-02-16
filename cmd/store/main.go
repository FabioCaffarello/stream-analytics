// Package main is the market-raccoon store binary.
//
// The store is the cold-path (ClickHouse) authority for ack-on-commit.
// S0 (skeleton): health/metrics/config endpoints only, no data writing.
//
// v0 wiring:
//
//	engine
//	  └─ Guardian  (observer mode — no subsystem factories)
//	HTTP (net/http)
//	  GET  /healthz           → 200 ok                (liveness)
//	  GET  /readyz            → 200/503 ready state   (readiness)
//	  GET  /runtime/snapshot  → JSON guardian state
//	  POST /runtime/reload    → 202 accepted
//	  GET  /metrics           → Prometheus metrics
//
// Usage:
//
//	go run ./cmd/store [flags]
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

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	"github.com/market-raccoon/internal/shared/config"
)

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	addrOverride := flag.String("addr", "", "HTTP listen address (overrides config)")
	logLevelOverride := flag.String("log-level", "", "log level override: debug|info|warn|error")
	busOverride := flag.String("bus", "", "bus type override: inmemory|jetstream")
	flag.Parse()

	cfg := loadStoreConfig(*configPath, *addrOverride, *logLevelOverride, *busOverride)

	logger := buildLogger(cfg.Log)
	slog.SetDefault(logger)
	logger.Info("store starting", "addr", cfg.HTTP.Addr)

	// ── engine ────────────────────────────────────────────────────────────────
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		logger.Error("failed to create actor engine", "err", err)
		os.Exit(1)
	}

	// ── guardian (observer mode — no subsystem factories) ────────────────────
	guardianPID := e.Spawn(
		actorruntime.NewGuardian(actorruntime.GuardianConfig{
			Logger:             logger,
			Factories:          map[actorruntime.Subsystem]actor.Producer{},
			ExpectedSubsystems: []actorruntime.Subsystem{},
		}),
		"guardian",
		actor.WithID("guardian"),
	)
	logger.Info("store: guardian spawned", "pid", guardianPID.String())

	// ── HTTP server ───────────────────────────────────────────────────────────
	srv := httpserver.NewServer(e, guardianPID, cfg.HTTP.Addr, cfg.HTTP.EnablePprof, logger)

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
		logger.Info("store: received signal", "signal", sig)
	case err := <-serverErr:
		logger.Error("store: HTTP server error", "err", err)
	}

	logger.Info("store: shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer cancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Warn("store: HTTP shutdown error", "err", err)
	}

	e.Send(guardianPID, actorruntime.Stop{})
	select {
	case <-e.Poison(guardianPID).Done():
	case <-shutCtx.Done():
		logger.Warn("store: guardian did not stop in time")
	}
	logger.Info("store: shutdown complete")
}

func loadStoreConfig(configPath, addrOverride, logLevelOverride, busOverride string) config.AppConfig {
	cfg, prob := config.Load(configPath)
	if prob != nil {
		slog.Error("store: config load failed", "err", prob)
		os.Exit(1)
	}
	if addrOverride != "" {
		cfg.HTTP.Addr = addrOverride
	}
	if logLevelOverride != "" {
		cfg.Log.Level = logLevelOverride
	}
	if busOverride != "" {
		cfg.Bus.Type = busOverride
	}
	if prob = cfg.Validate(); prob != nil {
		slog.Error("store: config validation failed", "err", prob)
		os.Exit(1)
	}
	return cfg
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
