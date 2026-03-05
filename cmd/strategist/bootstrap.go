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
	signalsruntime "github.com/market-raccoon/internal/actors/signals/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	signalsapp "github.com/market-raccoon/internal/core/signals/app"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	defaultStrategistQueueCapacity = 1024
	strategistServiceName          = "strategist"
)

type strategistEnvelopeSource struct {
	envelopeCh <-chan envelope.Envelope
	consumeErr <-chan *problem.Problem
	shutdownFn func(context.Context)
}

// Run is the strategist composition root. It wires the signal composer
// subsystem, envelope source, and HTTP runtime endpoints.
func Run(ctx context.Context, cfg config.AppConfig, configPath string) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)
	contracts.SetProtoRolloutConfig(cfg.ProtoRollout.EventTypeFlags())
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		return fmt.Errorf("payload codec registry bootstrap: %v", p)
	}

	publisher, closePublisher, err := buildStrategistPublisher(cfg, logger)
	if err != nil {
		return err
	}

	source, err := initStrategistEnvelopeSource(cfg, logger)
	if err != nil {
		_ = closePublisher(context.Background())
		return err
	}

	composePolicy := signalsapp.DefaultComposePolicy()
	composePolicy.CorrelationWindowMs = cfg.Signals.CorrelationWindowMs
	limiterPolicy := signalsapp.DefaultRateLimitPolicy()
	limiterPolicy.DedupWindowMs = cfg.Signals.DedupWindowMs
	limiterPolicy.DedupCapPerKey = cfg.Signals.WindowCap
	limiterPolicy.RateLimitPerMin = cfg.Signals.RateLimitPerMin
	limiterPolicy.GlobalRateLimitMin = cfg.Signals.GlobalRateLimitPerMin

	strategistCfg := signalsruntime.SubsystemConfig{
		Logger:                logger.With("subsystem", "strategist"),
		EnvelopeCh:            source.envelopeCh,
		Composer:              signalsapp.NewSignalComposer(composePolicy),
		Limiter:               signalsapp.NewSignalRateLimiter(limiterPolicy),
		RegimeCacheMaxStreams: cfg.Evidence.RegimeMaxStreams,
		Publisher:             publisher,
		ReplicaID:             cfg.Shard.Index,
		ReplicaCount:          cfg.Shard.Count,
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
			actorruntime.SubsystemSignals: signalsruntime.NewSubsystemActor(strategistCfg),
		},
	})
	logger.Info("strategist: guardian spawned", "pid", guardianPID.String())

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
		logger.Info("strategist: received signal", "signal", sig)
	case err := <-serverErr:
		logger.Error("strategist: HTTP server error", "err", err)
	case p := <-source.consumeErr:
		if p != nil {
			logger.Error("strategist: consume loop failed", "err", p)
		}
	case <-ctx.Done():
		logger.Info("strategist: context canceled")
	}

	ready.Store(false)
	logger.Info("strategist: shutting down")

	httpShutCtx, httpCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer httpCancel()
	if err := srv.Shutdown(httpShutCtx); err != nil {
		logger.Warn("strategist: HTTP shutdown error", "err", err)
	}

	depsCtx, depsCancel := context.WithTimeout(context.Background(), cfg.HTTP.GuardianShutdownTimeoutDuration())
	defer depsCancel()
	source.shutdownFn(depsCtx)
	if p := closePublisher(depsCtx); p != nil {
		logger.Warn("strategist: publisher close failed", "err", p)
	}
	actorruntime.ShutdownGuardian(depsCtx, e, guardianPID, logger)
	logger.Info("strategist: shutdown complete")
	return nil
}

func buildStrategistPublisher(cfg config.AppConfig, logger *slog.Logger) (signalsruntime.EventPublisher, func(context.Context) *problem.Problem, error) {
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
			return nil, nil, fmt.Errorf("strategist: jetstream publisher init failed: %v", p)
		}
		return pub, pub.Close, nil
	}

	logPub := bus.NewLogPublisher(logger)
	return logPub, func(context.Context) *problem.Problem { return nil }, nil
}

func initStrategistEnvelopeSource(cfg config.AppConfig, logger *slog.Logger) (strategistEnvelopeSource, error) {
	if !strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
		logger.Info("strategist: bus.type is not jetstream, no inbound stream configured")
		return strategistEnvelopeSource{
			envelopeCh: make(chan envelope.Envelope),
			consumeErr: make(chan *problem.Problem),
			shutdownFn: func(context.Context) {},
		}, nil
	}

	queueCap := cfg.Processor.BusCapacity
	if queueCap <= 0 {
		queueCap = defaultStrategistQueueCapacity
	}
	filters := effectiveStrategistFilters(cfg.JetStream.FilterSubjects)
	durable := serviceDurable(cfg.JetStream.ConsumerDurable, strategistServiceName, cfg.Shard.Index, cfg.Shard.Count)

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
		return strategistEnvelopeSource{}, fmt.Errorf("strategist: jetstream consumer init failed: %v", p)
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
				return problem.WithRetryable(problem.Wrap(consumeCtx.Err(), problem.Unavailable, "strategist enqueue canceled"))
			default:
				return problem.WithRetryable(problem.New(problem.Unavailable, "strategist queue full"))
			}
		})
	}()

	logger.Info("strategist: subscribed to jetstream consumer",
		"url", cfg.JetStream.URL,
		"stream", cfg.JetStream.StreamName,
		"durable", durable,
		"filters", filters,
		"shard", fmt.Sprintf("%d/%d", cfg.Shard.Index, cfg.Shard.Count),
	)

	return strategistEnvelopeSource{
		envelopeCh: envCh,
		consumeErr: errCh,
		shutdownFn: func(shutCtx context.Context) {
			cancel()
			if p := consumer.Close(shutCtx); p != nil {
				logger.Warn("strategist: jetstream consumer close failed", "err", p)
			}
		},
	}, nil
}

func effectiveStrategistFilters(base []string) []string {
	out := append([]string(nil), base...)
	out = appendFilterSubjectIfMissing(out, "insights.>")
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
		logger.Info("strategist: proto rollout flags reloaded", "config", configPath)
		return nil
	}
}
