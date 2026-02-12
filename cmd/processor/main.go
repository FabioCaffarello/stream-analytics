// Package main is the market-raccoon processor binary.
//
// The processor subscribes to an InMemoryBus, reads normalised event envelopes,
// and applies them to the core aggregation use cases (order book, etc.).
//
// v1 wiring:
//
//	InMemoryBus (channel)
//	  └─ ProcessorSubsystemActor  → UpdateOrderBookFromEvents → LogArtifactPublisher
//	engine
//	  └─ Guardian (aggregation factory)
//
// In production, the InMemoryBus will be replaced by NATS JetStream
// (see ADR-0004).  The ProcessorSubsystemActor only depends on a
// <-chan envelope.Envelope, so the swap is a one-line change in this wiring.
//
// Usage:
//
//	go run ./cmd/processor [flags]
//	  -config     string  path to JSONC config file (default "config.jsonc")
//	  -log-level  string  log level override: debug|info|warn|error
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/anthdm/hollywood/actor"
	aggruntime "github.com/market-raccoon/internal/actors/aggregation/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// v1 stub adapters for aggregation ports
// (will be replaced by real NATS/storage adapters in a future iteration)
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

type noopHotStore struct{}

func (n *noopHotStore) Save(_ context.Context, _ aggdomain.SnapshotProduced) *problem.Problem {
	return nil
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	logLevelOverride := flag.String("log-level", "", "log level override: debug|info|warn|error")
	flag.Parse()

	// ── config ─────────────────────────────────────────────────────────────
	cfg, prob := config.Load(*configPath)
	if prob != nil {
		slog.Error("processor: config load failed", "err", prob)
		os.Exit(1)
	}
	if *logLevelOverride != "" {
		cfg.Log.Level = *logLevelOverride
	}
	if prob = cfg.Validate(); prob != nil {
		slog.Error("processor: config validation failed", "err", prob)
		os.Exit(1)
	}

	// ── logger ─────────────────────────────────────────────────────────────
	logger := buildLogger(cfg.Log)
	slog.SetDefault(logger)

	logger.Info("processor starting")

	// ── event bus (in-memory v1) ────────────────────────────────────────────
	// In production: subscribe to NATS JetStream consumer instead.
	eventBus := bus.NewInMemoryBus(cfg.Processor.BusCapacity)
	envelopeCh := eventBus.Subscribe()

	logger.Info("processor: subscribed to in-memory bus")

	// ── aggregation use case ────────────────────────────────────────────────
	artifactPub := &logArtifactPublisher{logger: logger}
	hotStore := &noopHotStore{}
	updateBook := aggapp.NewUpdateOrderBookFromEvents(artifactPub, hotStore)

	// ── processor subsystem config ──────────────────────────────────────────
	processorCfg := aggruntime.ProcessorConfig{
		Logger:     logger,
		EnvelopeCh: envelopeCh,
		UpdateBook: updateBook,
	}

	// ── engine ──────────────────────────────────────────────────────────────
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		logger.Error("failed to create actor engine", "err", err)
		os.Exit(1)
	}

	// ── guardian with aggregation factory ──────────────────────────────────
	guardianPID := e.Spawn(
		actorruntime.NewGuardian(actorruntime.GuardianConfig{
			Logger: logger,
			Factories: map[actorruntime.Subsystem]actor.Producer{
				actorruntime.SubsystemAggregation: aggruntime.NewProcessorSubsystemActor(processorCfg),
			},
		}),
		"guardian",
		actor.WithID("guardian"),
	)
	logger.Info("processor: guardian spawned", "pid", guardianPID.String())
	logger.Info("processor: waiting for envelopes (use cmd/consumer or inject via InMemoryBus)")

	// ── signal handling ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("processor: shutting down")
	eventBus.Close()

	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer cancel()

	e.Send(guardianPID, actorruntime.Stop{})
	select {
	case <-e.Poison(guardianPID).Done():
	case <-shutCtx.Done():
		logger.Warn("processor: guardian did not stop in time")
	}
	logger.Info("processor: shutdown complete")
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
