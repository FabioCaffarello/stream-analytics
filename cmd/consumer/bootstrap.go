package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anthdm/hollywood/actor"
	mdruntime "github.com/market-raccoon/internal/actors/marketdata/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	mdapp "github.com/market-raccoon/internal/core/marketdata/app"
	"github.com/market-raccoon/internal/core/marketdata/ports"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
)

// ---------------------------------------------------------------------------
// in-memory sequencer (v1 — replaced by a distributed sequencer in v2)
// ---------------------------------------------------------------------------

type inMemSequencer struct {
	mu  sync.Mutex
	seq map[string]int64
}

func newInMemSequencer() *inMemSequencer {
	return &inMemSequencer{seq: make(map[string]int64)}
}

func (s *inMemSequencer) Next(venue, instrument string) (int64, *problem.Problem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := venue + ":" + instrument
	s.seq[key]++
	return s.seq[key], nil
}

// ---------------------------------------------------------------------------
// Run — consumer composition root
// ---------------------------------------------------------------------------

// Run is the consumer composition root.  It wires exchange adapters, the
// ingest use case, guardian, and blocks until a signal arrives.
func Run(ctx context.Context, cfg config.AppConfig) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)

	// Replay mode is a separate, offline codepath.
	if strings.TrimSpace(cfg.MarketData.ReplayPath) != "" {
		runConsumerReplay(cfg, logger)
		return nil
	}

	e2e, p := newE2ERuntime(logger)
	if p != nil {
		return fmt.Errorf("invalid e2e runtime posture: %v", p)
	}

	logger.Info("consumer starting",
		"shard", fmt.Sprintf("%d/%d", cfg.Shard.Index, cfg.Shard.Count),
		"bus_type", cfg.Bus.Type,
		"streams_per_ticker", cfg.Consumer.StreamsPerTicker,
		"max_streams_per_websocket", cfg.Consumer.MaxStreamsPerWebsocket,
		"max_websockets", cfg.Consumer.MaxWebsockets,
		"backpressure_buffer_size", cfg.Consumer.BackpressureBufferSize,
		"backpressure_policy", cfg.Consumer.BackpressurePolicy,
		"reconnect_base_backoff", cfg.Consumer.ReconnectBaseBackoff,
		"reconnect_max_backoff", cfg.Consumer.ReconnectMaxBackoff,
		"reconnect_budget_window", cfg.Consumer.ReconnectBudgetWindow,
		"reconnect_retry_budget", cfg.Consumer.ReconnectRetryBudget,
		"reconnect_cooldown", cfg.Consumer.ReconnectCooldown,
	)

	// ── dependencies ─────────────────────────────────────────────────────
	pub, closePublisher := buildPublisher(cfg, logger)
	pub, closePublisher = wrapWithRecorderPublisher(cfg, logger, pub, closePublisher)
	seq := newInMemSequencer()
	clk := clock.NewSystemClock()
	mdService := &mdapp.MarketDataService{
		Ingest: mdapp.NewIngestMarketDataWithConfig(clk, seq, pub, mdapp.IngestConfig{
			MaxStreams:         cfg.MarketData.MaxInstruments,
			PublishContentType: cfg.MarketData.PublishContentType,
		}),
	}

	runtimes, p := buildExchangeRuntimes(cfg, logger)
	if p != nil {
		return fmt.Errorf("exchange runtime build failed: %v", p)
	}
	for _, runtimeCfg := range runtimes {
		logger.Info("consumer exchange configured",
			"subsystem", runtimeCfg.Subsystem,
			"name", runtimeCfg.Exchange.Name,
			"type", runtimeCfg.Exchange.Type,
			"market_type", runtimeCfg.Exchange.MarketType,
			"tickers", runtimeCfg.Exchange.Tickers,
			"base_url", runtimeCfg.Exchange.BaseURL,
		)
	}

	// ── engine ───────────────────────────────────────────────────────────
	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		return err
	}
	e2e.bindEngine(e)

	// ── guardian ─────────────────────────────────────────────────────────
	factories := make(map[actorruntime.Subsystem]actor.Producer, len(runtimes))
	expected := make([]actorruntime.Subsystem, 0, len(runtimes))
	for _, runtimeCfg := range runtimes {
		managerCfg := runtimeCfg.ManagerCfg
		if e2e.isEnabled() {
			managerCfg = nil
		}
		subCfg := mdruntime.SubsystemConfig{
			Subsystem:              runtimeCfg.Subsystem,
			Logger:                 logger,
			Service:                mdService,
			ParseMessage:           runtimeCfg.ParseV1,
			ParseMessageV2:         runtimeCfg.ParseV2,
			ManagerConfig:          managerCfg,
			OnStarted:              e2e.subsystemStartedHook(runtimeCfg.Exchange.Type, runtimeCfg.Exchange.Name),
			BackpressureBufferSize: cfg.Consumer.BackpressureBufferSize,
			BackpressurePolicy:     cfg.Consumer.BackpressurePolicy,
		}
		factories[runtimeCfg.Subsystem] = mdruntime.NewSubsystemActor(subCfg)
		expected = append(expected, runtimeCfg.Subsystem)
	}

	guardianPID := actorruntime.SpawnGuardian(e, actorruntime.GuardianConfig{
		Logger:             logger,
		Factories:          factories,
		ExpectedSubsystems: expected,
	})
	logger.Info("guardian spawned", "pid", guardianPID.String())
	e2e.bindGuardian(guardianPID)
	if p := e2e.startProbe(); p != nil {
		return fmt.Errorf("failed to start e2e probe: %v", p)
	}

	// ── signal handling ──────────────────────────────────────────────────
	quit := bootstrap.SignalChannel()
	select {
	case <-quit:
	case <-ctx.Done():
	}

	logger.Info("consumer: shutting down")

	shutdownConsumerRuntime(
		logger,
		consumerShutdownHooks{
			shutdownE2E: e2e.shutdown,
			closePublisher: func(shutCtx context.Context) *problem.Problem {
				return closePublisher(shutCtx)
			},
			stopGuardian: func() {
				e.Send(guardianPID, actorruntime.Stop{})
			},
			waitGuardianStopped: func(shutCtx context.Context) bool {
				select {
				case <-e.Poison(guardianPID).Done():
					return true
				case <-shutCtx.Done():
					return false
				}
			},
		},
		cfg.HTTP.PublisherFlushTimeoutDuration(),
		cfg.HTTP.GuardianShutdownTimeoutDuration(),
	)
	return nil
}

// ---------------------------------------------------------------------------
// shutdown
// ---------------------------------------------------------------------------

type consumerShutdownHooks struct {
	shutdownE2E         func(context.Context) *problem.Problem
	closePublisher      func(context.Context) *problem.Problem
	stopGuardian        func()
	waitGuardianStopped func(context.Context) bool
}

func shutdownConsumerRuntime(
	logger *slog.Logger,
	hooks consumerShutdownHooks,
	publisherFlushTimeout time.Duration,
	guardianShutdownTimeout time.Duration,
) {
	depsCtx, depsCancel := context.WithTimeout(context.Background(), guardianShutdownTimeout)
	defer depsCancel()
	if p := hooks.shutdownE2E(depsCtx); p != nil {
		logger.Warn("consumer: e2e probe shutdown failed", "err", p)
	}

	flushCtx, flushCancel := context.WithTimeout(context.Background(), publisherFlushTimeout)
	if p := hooks.closePublisher(flushCtx); p != nil {
		logger.Warn("consumer: publisher close failed", "err", p)
	}
	flushCancel()

	guardianCtx, guardianCancel := context.WithTimeout(context.Background(), guardianShutdownTimeout)
	defer guardianCancel()
	hooks.stopGuardian()
	if !hooks.waitGuardianStopped(guardianCtx) {
		logger.Warn("consumer: guardian did not stop in time")
	}
	logger.Info("consumer: shutdown complete")
}

// ---------------------------------------------------------------------------
// publisher wiring
// ---------------------------------------------------------------------------

func buildPublisher(cfg config.AppConfig, logger *slog.Logger) (ports.EventPublisher, func(context.Context) *problem.Problem) {
	switch strings.ToLower(strings.TrimSpace(cfg.Bus.Type)) {
	case "jetstream":
		pub, p := adapterjs.NewPublisher(context.Background(), adapterjs.PublisherConfig{
			URL:            cfg.JetStream.URL,
			StreamName:     cfg.JetStream.StreamName,
			DedupWindow:    cfg.JetStream.DedupWindowDuration(),
			MaxAge:         cfg.JetStream.MaxAgeDuration(),
			MaxBytes:       cfg.JetStream.MaxBytesInt64(),
			PublishTimeout: 5 * time.Second,
		}, metrics.NewBusObserver())
		if p != nil {
			logger.Error("consumer: jetstream publisher init failed", "err", p)
			os.Exit(1)
		}
		logger.Info("consumer: using jetstream publisher",
			"url", cfg.JetStream.URL,
			"stream", cfg.JetStream.StreamName,
			"dedup_window", cfg.JetStream.DedupWindow,
			"max_age", cfg.JetStream.MaxAge,
			"max_bytes", cfg.JetStream.MaxBytes,
		)
		return pub, pub.Close
	default:
		logger.Info("consumer: using in-memory/log publisher")
		return bus.NewLogPublisher(logger), func(context.Context) *problem.Problem { return nil }
	}
}

func wrapWithRecorderPublisher(
	cfg config.AppConfig,
	logger *slog.Logger,
	pub ports.EventPublisher,
	closePublisher func(context.Context) *problem.Problem,
) (ports.EventPublisher, func(context.Context) *problem.Problem) {
	recordPath := strings.TrimSpace(cfg.MarketData.RecordPath)
	if recordPath == "" {
		return pub, closePublisher
	}

	recPub, p := replay.NewRecorderPublisher(pub, recordPath)
	if p != nil {
		logger.Error("consumer: recorder init failed", "record_path", recordPath, "err", p)
		os.Exit(1)
	}
	logger.Info("consumer: fixture recording enabled", "record_path", recordPath)

	return recPub, func(ctx context.Context) *problem.Problem {
		var first *problem.Problem
		if p := recPub.Close(); p != nil {
			first = p
		}
		if p := closePublisher(ctx); p != nil && first == nil {
			first = p
		}
		return first
	}
}
