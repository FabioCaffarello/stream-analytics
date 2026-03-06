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
	portfolioruntime "github.com/market-raccoon/internal/actors/portfolio/runtime"
	actorruntime "github.com/market-raccoon/internal/actors/runtime"
	"github.com/market-raccoon/internal/adapters/bus"
	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	portfolioapp "github.com/market-raccoon/internal/core/portfolio/app"
	httpserver "github.com/market-raccoon/internal/interfaces/http"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/contracts"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/metrics"
	"github.com/market-raccoon/internal/shared/problem"
)

const (
	defaultPortfolioQueueCapacity = 1024
	portfolioServiceName          = "portfolio"
)

type portfolioEnvelopeSource struct {
	envelopeCh <-chan envelope.Envelope
	consumeErr <-chan *problem.Problem
	shutdownFn func(context.Context)
}

func Run(ctx context.Context, cfg config.AppConfig, configPath string) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)
	contracts.SetProtoRolloutConfig(cfg.ProtoRollout.EventTypeFlags())
	if p := contracts.BootstrapPayloadCodecRegistry(); p != nil {
		return fmt.Errorf("payload codec registry bootstrap: %v", p)
	}

	publisher, closePublisher, err := buildPortfolioPublisher(cfg, logger)
	if err != nil {
		return err
	}

	source, err := initPortfolioEnvelopeSource(cfg, logger)
	if err != nil {
		_ = closePublisher(context.Background())
		return err
	}

	portfolioCfg := portfolioruntime.SubsystemConfig{
		Logger:       logger.With("subsystem", "portfolio"),
		EnvelopeCh:   source.envelopeCh,
		Projector:    portfolioapp.NewBootstrapProjector(portfolioapp.DefaultProjectorConfig()),
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
			actorruntime.SubsystemPortfolio: portfolioruntime.NewSubsystemActor(portfolioCfg),
		},
	})
	logger.Info("portfolio: guardian spawned", "pid", guardianPID.String())

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
		logger.Info("portfolio: received signal", "signal", sig)
	case err := <-serverErr:
		logger.Error("portfolio: HTTP server error", "err", err)
	case p := <-source.consumeErr:
		if p != nil {
			logger.Error("portfolio: consume loop failed", "err", p)
		}
	case <-ctx.Done():
		logger.Info("portfolio: context canceled")
	}

	ready.Store(false)
	logger.Info("portfolio: shutting down")

	httpShutCtx, httpCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer httpCancel()
	if err := srv.Shutdown(httpShutCtx); err != nil {
		logger.Warn("portfolio: HTTP shutdown error", "err", err)
	}

	depsCtx, depsCancel := context.WithTimeout(context.Background(), cfg.HTTP.GuardianShutdownTimeoutDuration())
	defer depsCancel()
	source.shutdownFn(depsCtx)
	if p := closePublisher(depsCtx); p != nil {
		logger.Warn("portfolio: publisher close failed", "err", p)
	}
	actorruntime.ShutdownGuardian(depsCtx, e, guardianPID, logger)
	logger.Info("portfolio: shutdown complete")
	return nil
}

func buildPortfolioPublisher(cfg config.AppConfig, logger *slog.Logger) (portfolioruntime.EventPublisher, func(context.Context) *problem.Problem, error) {
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
			return nil, nil, fmt.Errorf("portfolio: jetstream publisher init failed: %v", p)
		}
		return pub, pub.Close, nil
	}

	logPub := bus.NewLogPublisher(logger)
	return logPub, func(context.Context) *problem.Problem { return nil }, nil
}

func initPortfolioEnvelopeSource(cfg config.AppConfig, logger *slog.Logger) (portfolioEnvelopeSource, error) {
	if !strings.EqualFold(strings.TrimSpace(cfg.Bus.Type), "jetstream") {
		logger.Info("portfolio: bus.type is not jetstream, no inbound stream configured")
		return portfolioEnvelopeSource{
			envelopeCh: make(chan envelope.Envelope),
			consumeErr: make(chan *problem.Problem),
			shutdownFn: func(context.Context) {},
		}, nil
	}

	queueCap := cfg.Processor.BusCapacity
	if queueCap <= 0 {
		queueCap = defaultPortfolioQueueCapacity
	}
	filters := effectivePortfolioFilters(cfg.JetStream.FilterSubjects)
	durable := serviceDurable(cfg.JetStream.ConsumerDurable, portfolioServiceName, cfg.Shard.Index, cfg.Shard.Count)

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
		return portfolioEnvelopeSource{}, fmt.Errorf("portfolio: jetstream consumer init failed: %v", p)
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
				return problem.WithRetryable(problem.Wrap(consumeCtx.Err(), problem.Unavailable, "portfolio enqueue canceled"))
			default:
				return problem.WithRetryable(problem.New(problem.Unavailable, "portfolio queue full"))
			}
		})
	}()

	logger.Info("portfolio: subscribed to jetstream consumer",
		"url", cfg.JetStream.URL,
		"stream", cfg.JetStream.StreamName,
		"durable", durable,
		"filters", filters,
		"shard", fmt.Sprintf("%d/%d", cfg.Shard.Index, cfg.Shard.Count),
	)

	return portfolioEnvelopeSource{
		envelopeCh: envCh,
		consumeErr: errCh,
		shutdownFn: func(shutCtx context.Context) {
			cancel()
			if p := consumer.Close(shutCtx); p != nil {
				logger.Warn("portfolio: jetstream consumer close failed", "err", p)
			}
		},
	}, nil
}

func effectivePortfolioFilters(base []string) []string {
	const (
		canonicalFamily = "execution.event"
		canonicalProbe  = "execution.event.v1.binance.BTCUSDT"
	)
	out := make([]string, 0, len(base)+1)
	for _, raw := range base {
		subject := strings.TrimSpace(raw)
		if subject == "" {
			continue
		}
		if subjectMatchesFilter(canonicalProbe, subject) && filterTargetsEventFamily(subject, canonicalFamily) {
			out = append(out, subject)
		}
	}
	out = appendFilterSubjectIfMissing(out, canonicalFamily+".>")
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

func filterTargetsEventFamily(filter, family string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	family = strings.ToLower(strings.TrimSpace(family))
	if filter == "" || family == "" {
		return false
	}
	if filter == family || strings.HasPrefix(filter, family+".") {
		return true
	}
	if strings.HasSuffix(filter, ".>") {
		prefix := strings.TrimSuffix(filter, ".>")
		return prefix == family || strings.HasPrefix(prefix, family+".")
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
		logger.Info("portfolio: proto rollout flags reloaded", "config", configPath)
		return nil
	}
}
