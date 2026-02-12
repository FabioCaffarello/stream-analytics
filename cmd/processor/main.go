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
//	  -bus        string  bus adapter override: inmemory|jetstream
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/anthdm/hollywood/actor"
	aggruntime "github.com/market-raccoon/internal/actors/aggregation/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	aggapp "github.com/market-raccoon/internal/core/aggregation/app"
	aggdomain "github.com/market-raccoon/internal/core/aggregation/domain"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/envelope"
	sharedhash "github.com/market-raccoon/internal/shared/hash"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
	"github.com/market-raccoon/internal/shared/replay"
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

type envelopeSource struct {
	envelopeCh <-chan envelope.Envelope
	consumeErr <-chan *problem.Problem
	shutdownFn func(context.Context)
	onResult   func(aggruntime.EnvelopeProcessResult)
}

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
		resultsCh := make(chan aggruntime.EnvelopeProcessResult, 1)

		onResult := func(res aggruntime.EnvelopeProcessResult) {
			res.Problem = e2e.maybeInjectTransient(res.Envelope, res.Problem)
			select {
			case resultsCh <- res:
			default:
				logger.Warn("processor: dropped processing result notification")
			}
		}

		jetstreamConsumer, p := adapterjs.NewConsumer(context.Background(), adapterjs.ConsumerConfig{
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
			logger.Error("processor: jetstream consumer init failed", "err", p)
			os.Exit(1)
		}

		runCtx, cancelConsume := context.WithCancel(context.Background())
		errCh := make(chan *problem.Problem, 1)
		go func() {
			errCh <- jetstreamConsumer.Consume(runCtx, func(ctx context.Context, env envelope.Envelope) *problem.Problem {
				select {
				case ch <- env:
				case <-ctx.Done():
					return problem.WithRetryable(problem.Wrap(ctx.Err(), problem.Unavailable, "processor enqueue canceled"))
				}
				select {
				case res := <-resultsCh:
					return res.Problem
				case <-ctx.Done():
					return problem.WithRetryable(problem.Wrap(ctx.Err(), problem.Unavailable, "processor result wait canceled"))
				}
			})
		}()

		logger.Info("processor: subscribed to jetstream consumer",
			"url", cfg.JetStream.URL,
			"stream", cfg.JetStream.StreamName,
			"durable", cfg.JetStream.ConsumerDurable,
			"filters", cfg.JetStream.FilterSubjects,
		)

		return envelopeSource{
			envelopeCh: ch,
			consumeErr: errCh,
			onResult:   onResult,
			shutdownFn: func(ctx context.Context) {
				cancelConsume()
				if p := jetstreamConsumer.Close(ctx); p != nil {
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
			"sha256", sharedhash.HashFields(hashes...),
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
// main
// ---------------------------------------------------------------------------

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	logLevelOverride := flag.String("log-level", "", "log level override: debug|info|warn|error")
	busTypeOverride := flag.String("bus", "", "bus adapter override: inmemory|jetstream")
	replayModeOverride := flag.String("replay-mode", "", "replay mode override: off|file|jetstream")
	replayPathOverride := flag.String("replay-path", "", "optional fixture path to replay envelopes")
	flag.Parse()

	cfg := loadProcessorConfig(*configPath, *logLevelOverride, *busTypeOverride, *replayModeOverride, *replayPathOverride)

	// ── logger ─────────────────────────────────────────────────────────────
	logger := buildLogger(cfg.Log)
	slog.SetDefault(logger)
	e2e := newE2ERuntime(logger)
	if p := e2e.startProbe(); p != nil {
		logger.Error("processor: failed to start e2e probe", "err", p)
		os.Exit(1)
	}

	logger.Info("processor starting", "bus_type", cfg.Bus.Type)

	// ── aggregation use case ────────────────────────────────────────────────
	artifactPub := &logArtifactPublisher{logger: logger}
	hotStore := &noopHotStore{}
	updateBook := aggapp.NewUpdateOrderBookFromEvents(artifactPub, hotStore)

	// ── envelope source wiring ───────────────────────────────────────────────
	source := initEnvelopeSource(cfg, logger, e2e)

	// ── processor subsystem config ──────────────────────────────────────────
	processorCfg := aggruntime.ProcessorConfig{
		Logger:              logger,
		EnvelopeCh:          source.envelopeCh,
		UpdateBook:          updateBook,
		OnEnvelopeProcessed: source.onResult,
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
	e2e.markReady()

	// ── signal handling ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
	case p := <-source.consumeErr:
		if p != nil {
			logger.Error("processor: jetstream consume loop failed", "err", p)
		}
	}

	logger.Info("processor: shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer cancel()
	if p := e2e.shutdown(shutCtx); p != nil {
		logger.Warn("processor: e2e probe shutdown failed", "err", p)
	}
	source.shutdownFn(shutCtx)

	e.Send(guardianPID, actorruntime.Stop{})
	select {
	case <-e.Poison(guardianPID).Done():
	case <-shutCtx.Done():
		logger.Warn("processor: guardian did not stop in time")
	}
	logger.Info("processor: shutdown complete")
}

func loadProcessorConfig(configPath, logLevelOverride, busTypeOverride, replayModeOverride, replayPathOverride string) config.AppConfig {
	cfg, prob := config.Load(configPath)
	if prob != nil {
		slog.Error("processor: config load failed", "err", prob)
		os.Exit(1)
	}
	if logLevelOverride != "" {
		cfg.Log.Level = logLevelOverride
	}
	if busTypeOverride != "" {
		cfg.Bus.Type = busTypeOverride
	}
	if strings.TrimSpace(replayModeOverride) != "" {
		cfg.Replay.Mode = strings.TrimSpace(replayModeOverride)
	}
	if strings.TrimSpace(replayPathOverride) != "" {
		cfg.MarketData.ReplayPath = strings.TrimSpace(replayPathOverride)
		cfg.Replay.Mode = "file"
	}
	if prob = cfg.Validate(); prob != nil {
		slog.Error("processor: config validation failed", "err", prob)
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
