// Package main is the market-raccoon store binary.
//
// The store is the cold-path (ClickHouse) authority for ack-on-commit.
//
// S1 wiring:
//
//	engine
//	  └─ Guardian  (observer mode — no subsystem factories)
//	JetStream consumer  (durable "store-v1", filters aggregation.>)
//	  └─ handleStoreEnvelope  → decode + route + ClickHouse writer → ACK
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
//	  -bus        string   bus type override: inmemory|jetstream
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	"github.com/market-raccoon/internal/adapters/storage/clickhouse"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

// storeHeartbeatEveryN controls the heartbeat log interval.
const storeHeartbeatEveryN = 1000

// storeConsumedCount tracks total consumed messages for heartbeat logging.
var storeConsumedCount atomic.Uint64

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	addrOverride := flag.String("addr", "", "HTTP listen address (overrides config)")
	logLevelOverride := flag.String("log-level", "", "log level override: debug|info|warn|error")
	busOverride := flag.String("bus", "", "bus type override: inmemory|jetstream")
	flag.Parse()

	cfg := loadStoreConfig(*configPath, *addrOverride, *logLevelOverride, *busOverride)

	logger := buildLogger(cfg.Log)
	slog.SetDefault(logger)
	logger.Info("store starting", "addr", cfg.HTTP.Addr, "bus", cfg.Bus.Type)

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

	// ── ClickHouse writer (in-memory skeleton — batch pipeline TODO) ─────────
	chWriter := clickhouse.NewWriter()

	// ── JetStream consumer (when bus.type=jetstream) ─────────────────────────
	var consumeErr <-chan *problem.Problem
	var shutdownConsumer func(context.Context)
	if strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
		consumeErr, shutdownConsumer = initStoreConsumer(cfg, chWriter, logger)
	} else {
		logger.Info("store: bus.type is not jetstream, running in observer mode")
	}

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
	case p := <-consumeErr:
		if p != nil {
			logger.Error("store: consume loop failed", "err", p)
		}
	}

	logger.Info("store: shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer cancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Warn("store: HTTP shutdown error", "err", err)
	}

	if shutdownConsumer != nil {
		shutdownConsumer(shutCtx)
	}

	e.Send(guardianPID, actorruntime.Stop{})
	select {
	case <-e.Poison(guardianPID).Done():
	case <-shutCtx.Done():
		logger.Warn("store: guardian did not stop in time")
	}
	logger.Info("store: shutdown complete")
}

// initStoreConsumer creates a JetStream consumer for the store pipeline and
// starts consuming in a background goroutine.  Returns an error channel for
// fatal consume errors and a shutdown function.
func initStoreConsumer(cfg config.AppConfig, writer *clickhouse.Writer, logger *slog.Logger) (<-chan *problem.Problem, func(context.Context)) {
	jsConsumer, p := adapterjs.NewConsumer(context.Background(), adapterjs.ConsumerConfig{
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
		logger.Error("store: jetstream consumer init failed", "err", p)
		os.Exit(1)
	}

	consumeCtx, cancelConsume := context.WithCancel(context.Background())
	errCh := make(chan *problem.Problem, 1)

	go func() {
		errCh <- jsConsumer.Consume(consumeCtx, func(ctx context.Context, env envelope.Envelope) *problem.Problem {
			return handleStoreEnvelope(ctx, env, writer, logger)
		})
	}()

	logger.Info("store: subscribed to jetstream consumer",
		"url", cfg.JetStream.URL,
		"stream", cfg.JetStream.StreamName,
		"durable", cfg.JetStream.ConsumerDurable,
		"filters", cfg.JetStream.FilterSubjects,
	)

	return errCh, func(ctx context.Context) {
		cancelConsume()
		if p := jsConsumer.Close(ctx); p != nil {
			logger.Warn("store: jetstream consumer close failed", "err", p)
		}
	}
}

// handleStoreEnvelope routes an envelope to the appropriate write handler.
// For S2, only aggregation.snapshot.v1 is implemented; all other event types
// are ACKed with a skip metric.
func handleStoreEnvelope(ctx context.Context, env envelope.Envelope, writer *clickhouse.Writer, logger *slog.Logger) *problem.Problem {
	eventKey := fmt.Sprintf("%s.v%d", env.Type, env.Version)

	// Heartbeat log every N messages so operators can prove liveness.
	n := storeConsumedCount.Add(1)
	if n%storeHeartbeatEveryN == 0 {
		logger.Info("store: heartbeat", "consumed", n)
	}

	switch {
	case env.Type == "aggregation.snapshot" && env.Version == 1:
		p := handleAggregationSnapshot(ctx, env, writer, logger)
		if p != nil {
			metrics.IncStoreConsumed("failed", "commit")
		} else {
			metrics.IncStoreConsumed("ok", "snapshot")
		}
		return p
	default:
		metrics.IncStoreConsumed("ok", "skipped")
		logger.Debug("store: skipping unhandled event", "type", eventKey,
			"venue", env.Venue, "instrument", env.Instrument)
		return nil
	}
}

// handleAggregationSnapshot decodes an aggregation snapshot envelope and
// commits to the ClickHouse writer.  The JetStream consumer ACKs only after
// this function returns nil.
func handleAggregationSnapshot(ctx context.Context, env envelope.Envelope, writer *clickhouse.Writer, logger *slog.Logger) *problem.Problem {
	var snap aggdomain.SnapshotProduced
	if err := json.Unmarshal(env.Payload, &snap); err != nil {
		metrics.IncStoreQuarantine("decode")
		return problem.Wrap(err, problem.ValidationFailed, "store: decode aggregation.snapshot payload failed")
	}

	// Ensure BookID is populated from envelope metadata if the payload
	// omitted it (defensive against publisher variations).
	if snap.BookID.Venue == "" {
		snap.BookID.Venue = env.Venue
	}
	if snap.BookID.Instrument == "" {
		snap.BookID.Instrument = env.Instrument
	}
	if snap.Seq == 0 {
		snap.Seq = env.Seq
	}

	started := time.Now()
	if p := writer.Save(ctx, snap); p != nil {
		metrics.IncStoreCommit("failed")
		metrics.ObserveStoreCommitLatency(time.Since(started))
		// Return as-is — consumer's ClassifyIngestError checks p.Retryable
		// to decide NAK (transient) vs TERM (permanent).
		return p
	}

	metrics.IncStoreCommit("ok")
	metrics.ObserveStoreCommitLatency(time.Since(started))

	logger.Debug("store: snapshot committed",
		"venue", snap.BookID.Venue,
		"instrument", snap.BookID.Instrument,
		"seq", snap.Seq,
	)
	return nil
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
