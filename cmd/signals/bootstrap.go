package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/anthdm/hollywood/actor"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	signalruntime "github.com/market-raccoon/internal/actors/signal/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	signalcore "github.com/market-raccoon/internal/core/signal"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	defaultSignalQueueCapacity = 1024
	signalsServiceName         = "signals"
)

type signalEnvelopeSource struct {
	envelopeCh <-chan envelope.Envelope
	consumeErr <-chan *problem.Problem
	shutdownFn func(context.Context)
}

// Run is the signals composition root. It wires the signal subsystem,
// envelope source, and HTTP runtime endpoints.
func Run(ctx context.Context, cfg config.AppConfig, configPath string) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)
	contracts.SetProtoRolloutConfig(cfg.ProtoRollout.EventTypeFlags())
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		return fmt.Errorf("payload codec registry bootstrap: %v", p)
	}

	publisher, closePublisher, err := buildSignalPublisher(cfg, logger)
	if err != nil {
		return err
	}

	source, err := initSignalEnvelopeSource(cfg, logger)
	if err != nil {
		_ = closePublisher(context.Background())
		return err
	}

	signalCfg := signalruntime.SubsystemConfig{
		Logger:       logger.With("subsystem", "signals"),
		EnvelopeCh:   source.envelopeCh,
		Engine:       signalcore.NewSignalEngine(buildSignalEngineConfig(cfg), nil),
		Publisher:    publisher,
		ReplicaID:    cfg.Shard.Index,
		ReplicaCount: cfg.Shard.Count,
	}

	e, err := actorruntime.NewDefaultEngine()
	if err != nil {
		source.shutdownFn(context.Background())
		_ = closePublisher(context.Background())
		return err
	}

	guardianPID := actorruntime.SpawnGuardian(e, actorruntime.GuardianConfig{
		Logger: logger,
		Factories: map[actorruntime.Subsystem]actor.Producer{
			actorruntime.SubsystemSignals: signalruntime.NewSubsystemActor(signalCfg),
		},
	})
	logger.Info("signals: guardian spawned", "pid", guardianPID.String())

	var ready atomic.Bool
	ready.Store(true)

	srv := httpserver.NewServer(
		e,
		guardianPID,
		cfg.HTTP.Addr,
		cfg.HTTP.EnablePprof,
		logger,
		httpserver.WithTLS(cfg.HTTP.TLSCert, cfg.HTTP.TLSKey),
		httpserver.WithReloadHook(protoRolloutReloadHook(configPath, logger)),
	)
	srv.SetReadyGate(func() bool { return ready.Load() })

	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	quit := bootstrap.SignalChannel()
	select {
	case sig := <-quit:
		logger.Info("signals: received signal", "signal", sig)
	case err := <-serverErr:
		logger.Error("signals: HTTP server error", "err", err)
	case p := <-source.consumeErr:
		if p != nil {
			logger.Error("signals: consume loop failed", "err", p)
		}
	case <-ctx.Done():
		logger.Info("signals: context canceled")
	}

	ready.Store(false)
	logger.Info("signals: shutting down")

	httpShutCtx, httpCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer httpCancel()
	if err := srv.Shutdown(httpShutCtx); err != nil {
		logger.Warn("signals: HTTP shutdown error", "err", err)
	}

	depsCtx, depsCancel := context.WithTimeout(context.Background(), cfg.HTTP.GuardianShutdownTimeoutDuration())
	defer depsCancel()
	source.shutdownFn(depsCtx)
	if p := closePublisher(depsCtx); p != nil {
		logger.Warn("signals: publisher close failed", "err", p)
	}
	actorruntime.ShutdownGuardian(depsCtx, e, guardianPID, logger)
	logger.Info("signals: shutdown complete")
	return nil
}

func buildSignalPublisher(cfg config.AppConfig, logger *slog.Logger) (signalruntime.EventPublisher, func(context.Context) *problem.Problem, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
		pub, p := adapterjs.NewPublisher(context.Background(), adapterjs.PublisherConfig{
			URL:            cfg.JetStream.URL,
			StreamName:     cfg.JetStream.StreamName,
			DedupWindow:    cfg.JetStream.DedupWindowDuration(),
			MaxAge:         cfg.JetStream.MaxAgeDuration(),
			MaxBytes:       cfg.JetStream.MaxBytesInt64(),
			PublishTimeout: cfg.Processor.PublisherTimeoutDuration(),
		}, metrics.NewBusObserver())
		if p != nil {
			return nil, nil, fmt.Errorf("signals: jetstream publisher init failed: %v", p)
		}
		return pub, pub.Close, nil
	}

	logPub := bus.NewLogPublisher(logger)
	return logPub, func(context.Context) *problem.Problem { return nil }, nil
}

func initSignalEnvelopeSource(cfg config.AppConfig, logger *slog.Logger) (signalEnvelopeSource, error) {
	if !strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
		logger.Info("signals: bus.type is not jetstream, no inbound stream configured")
		return signalEnvelopeSource{
			envelopeCh: make(chan envelope.Envelope),
			consumeErr: make(chan *problem.Problem),
			shutdownFn: func(context.Context) {},
		}, nil
	}

	queueCap := cfg.Processor.BusCapacity
	if queueCap <= 0 {
		queueCap = defaultSignalQueueCapacity
	}
	filters := effectiveSignalFilters(cfg.JetStream.FilterSubjects)
	durable := serviceDurable(cfg.JetStream.ConsumerDurable, signalsServiceName, cfg.Shard.Index, cfg.Shard.Count)

	consumer, p := adapterjs.NewConsumer(context.Background(), adapterjs.ConsumerConfig{
		URL:             cfg.JetStream.URL,
		StreamName:      cfg.JetStream.StreamName,
		DedupWindow:     cfg.JetStream.DedupWindowDuration(),
		MaxAge:          cfg.JetStream.MaxAgeDuration(),
		MaxBytes:        cfg.JetStream.MaxBytesInt64(),
		ConsumerDurable: durable,
		FilterSubjects:  filters,
		AckWait:         cfg.JetStream.AckWaitDuration(),
		MaxAckPending:   cfg.JetStream.MaxAckPending,
		MaxDeliver:      cfg.JetStream.MaxDeliver,
		DeliverPolicy:   cfg.JetStream.DeliverPolicy,
		ShardGroupCount: cfg.JetStream.ShardGroupCount,
		ShardGroupID:    cfg.JetStream.ShardGroupID,
		MaxLag:          cfg.Shard.MaxLag,
	}, metrics.NewBusObserver())
	if p != nil {
		return signalEnvelopeSource{}, fmt.Errorf("signals: jetstream consumer init failed: %v", p)
	}

	envCh := make(chan envelope.Envelope, queueCap)
	errCh := make(chan *problem.Problem, 1)
	runCtx, cancel := context.WithCancel(context.Background())

	go func() {
		errCh <- consumer.Consume(runCtx, func(consumeCtx context.Context, env envelope.Envelope) *problem.Problem {
			select {
			case envCh <- env:
				return nil
			case <-consumeCtx.Done():
				return problem.WithRetryable(problem.Wrap(consumeCtx.Err(), problem.Unavailable, "signals enqueue canceled"))
			default:
				return problem.WithRetryable(problem.New(problem.Unavailable, "signals queue full"))
			}
		})
	}()

	logger.Info("signals: subscribed to jetstream consumer",
		"url", cfg.JetStream.URL,
		"stream", cfg.JetStream.StreamName,
		"durable", durable,
		"filters", filters,
		"shard", fmt.Sprintf("%d/%d", cfg.Shard.Index, cfg.Shard.Count),
	)

	return signalEnvelopeSource{
		envelopeCh: envCh,
		consumeErr: errCh,
		shutdownFn: func(shutCtx context.Context) {
			cancel()
			if p := consumer.Close(shutCtx); p != nil {
				logger.Warn("signals: jetstream consumer close failed", "err", p)
			}
		},
	}, nil
}

func buildSignalEngineConfig(cfg config.AppConfig) signalcore.EngineConfig {
	out := signalcore.DefaultEngineConfig()
	if cfg.Signals.WindowCap > 0 {
		out.Store.PerStreamWindow = cfg.Signals.WindowCap
	}
	if cfg.Evidence.RegimeMaxStreams > 0 {
		out.Store.PerTenantStreamCap = cfg.Evidence.RegimeMaxStreams
	}
	if cfg.Processor.MaxInstruments > 0 {
		out.Store.GlobalStreamCap = cfg.Processor.MaxInstruments
	}
	if cfg.Signals.DedupWindowMs > 0 {
		out.Store.DedupWindowMillis = cfg.Signals.DedupWindowMs
	}
	if cfg.Signals.RateLimitPerMin > 0 {
		out.Store.TenantRateLimitMin = cfg.Signals.RateLimitPerMin
	}
	if cfg.Signals.CorrelationWindowMs > 0 {
		out.Rules.RegimeChange.WindowMs = cfg.Signals.CorrelationWindowMs
		out.Rules.LiquidityCollapse.WindowMs = cfg.Signals.CorrelationWindowMs
		out.Rules.PersistentImbalance.WindowMs = cfg.Signals.CorrelationWindowMs
	}
	return out
}

func effectiveSignalFilters(base []string) []string {
	out := append([]string(nil), base...)
	out = appendFilterSubjectIfMissing(out, "marketdata.>")
	out = appendFilterSubjectIfMissing(out, "aggregation.>")
	out = appendFilterSubjectIfMissing(out, "insights.>")
	out = appendFilterSubjectIfMissing(out, "liquidity.>")
	return dedupeSubjects(out)
}

func dedupeSubjects(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		subject := strings.TrimSpace(raw)
		if subject == "" {
			continue
		}
		if _, ok := seen[subject]; ok {
			continue
		}
		seen[subject] = struct{}{}
		out = append(out, subject)
	}
	return out
}

func appendFilterSubjectIfMissing(base []string, subject string) []string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return base
	}
	for _, existing := range base {
		if subjectMatchesFilter(subject, strings.TrimSpace(existing)) {
			return base
		}
	}
	return append(base, subject)
}

func subjectMatchesFilter(subject, filter string) bool {
	subject = strings.TrimSpace(subject)
	filter = strings.TrimSpace(filter)
	if subject == "" || filter == "" {
		return false
	}
	if filter == ">" || subject == filter {
		return true
	}
	if strings.HasSuffix(filter, ".>") {
		prefix := strings.TrimSuffix(filter, ">")
		return strings.HasPrefix(subject, prefix)
	}
	return false
}

func serviceDurable(base, service string, index, count int) string {
	base = strings.TrimSpace(base)
	service = strings.TrimSpace(service)
	if base == "" {
		base = service + "-v1"
	} else if service != "" && !strings.Contains(base, "-"+service) {
		base = base + "-" + service
	}
	if count > 1 {
		return fmt.Sprintf("%s-s%d", base, index)
	}
	return base
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
		logger.Info("signals: proto rollout flags reloaded", "config", configPath)
		return nil
	}
}
