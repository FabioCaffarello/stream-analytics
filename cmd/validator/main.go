package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	adapterjs "github.com/market-raccoon/internal/adapters/jetstream"
	adapternats "github.com/market-raccoon/internal/adapters/nats"
	"github.com/market-raccoon/internal/application/dataplane"
	"github.com/market-raccoon/internal/application/runtimebootstrap"
	"github.com/market-raccoon/internal/application/validatorruntime"
	"github.com/market-raccoon/internal/shared/bootstrap"
	"github.com/market-raccoon/internal/shared/clock"
	"github.com/market-raccoon/internal/shared/config"
	"github.com/market-raccoon/internal/shared/envelope"
	"github.com/market-raccoon/internal/shared/problem"
)

func main() {
	configPath := flag.String("config", "config.jsonc", "path to JSONC config file")
	addrOverride := flag.String("addr", "", "HTTP listen address override")
	flag.Parse()

	cfg, prob := bootstrap.LoadAndValidate(*configPath, func(c *config.AppConfig) {
		if *addrOverride != "" {
			c.HTTP.Addr = *addrOverride
		}
	})
	if prob != nil {
		fmt.Fprintf(os.Stderr, "validator: config error: %v\n", prob)
		os.Exit(1)
	}
	if err := Run(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "validator: %v\n", err)
		os.Exit(1)
	}
}

func Run(ctx context.Context, cfg config.AppConfig) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)

	store, p := adapternats.NewRuntimeStore(ctx, cfg.JetStream.URL, cfg.DataPlane.StateBucket)
	if p != nil {
		return fmt.Errorf("validator: runtime store init failed: %v", p)
	}
	defer store.Close()

	resultStore, p := adapternats.NewResultStore(ctx, cfg.JetStream.URL, cfg.DataPlane.StateBucket, cfg.DataPlane.ResultLimit)
	if p != nil {
		return fmt.Errorf("validator: result store init failed: %v", p)
	}
	defer resultStore.Close()

	publisher, p := adapterjs.NewPublisher(context.Background(), adapterjs.PublisherConfig{
		URL:            cfg.JetStream.URL,
		StreamName:     cfg.JetStream.StreamName,
		DedupWindow:    cfg.JetStream.DedupWindowDuration(),
		MaxAge:         cfg.JetStream.MaxAgeDuration(),
		MaxBytes:       cfg.JetStream.MaxBytesInt64(),
		PublishTimeout: 5 * time.Second,
	}, nil)
	if p != nil {
		return fmt.Errorf("validator: publisher init failed: %v", p)
	}
	defer publisher.Close(context.Background())

	consumer, p := adapterjs.NewConsumer(ctx, adapterjs.ConsumerConfig{
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
	}, nil)
	if p != nil {
		return fmt.Errorf("validator: consumer init failed: %v", p)
	}
	defer consumer.Close(context.Background())

	var ready atomic.Bool
	srv := newHealthServer(cfg.HTTP.Addr, &ready)
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	processor := validatorruntime.NewProcessor(runtimebootstrap.New(store), clock.NewSystemClock(), publisher, resultStore)
	consumeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	consumeErr := make(chan *problem.Problem, 1)
	ready.Store(true)
	go func() {
		consumeErr <- consumer.Consume(consumeCtx, func(ctx context.Context, env envelope.Envelope) *problem.Problem {
			msg, p := dataplane.MessageFromEnvelope(env)
			if p != nil {
				logger.Warn("validator: message decode failed", "err", p)
				return p
			}
			result, p := processor.Process(ctx, msg)
			if p == nil {
				logger.Info("validator: validation processed",
					"binding", result.Binding,
					"message_id", result.MessageID,
					"status", result.Status,
					"violations", len(result.Violations),
				)
			}
			return p
		})
	}()

	quit := bootstrap.SignalChannel()
	select {
	case err := <-serverErr:
		return fmt.Errorf("validator: http server failed: %w", err)
	case p := <-consumeErr:
		if p != nil {
			return fmt.Errorf("validator: consume loop failed: %v", p)
		}
	case <-quit:
	case <-ctx.Done():
	}

	cancel()
	shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer shutCancel()
	return srv.Shutdown(shutCtx)
}

func newHealthServer(addr string, ready *atomic.Bool) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if ready != nil && ready.Load() {
			_, _ = w.Write([]byte(`{"ready":true}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"ready":false}`))
	})
	return &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}
}
