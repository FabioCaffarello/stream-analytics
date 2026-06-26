package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	adapterjs "github.com/FabioCaffarello/stream-analytics/internal/adapters/jetstream"
	adapterkafka "github.com/FabioCaffarello/stream-analytics/internal/adapters/kafka"
	adapternats "github.com/FabioCaffarello/stream-analytics/internal/adapters/nats"
	"github.com/FabioCaffarello/stream-analytics/internal/application/dataplane"
	"github.com/FabioCaffarello/stream-analytics/internal/application/runtimebootstrap"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/bootstrap"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/config"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type dataPlaneEnvelopePublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

//nolint:gocyclo // Pipeline setup branches on optional Kafka, readers, and signals — splitting further adds no clarity.
func RunDataPlane(ctx context.Context, cfg config.AppConfig) error {
	logger := bootstrap.BuildLogger(cfg.Log)
	slog.SetDefault(logger)

	if len(cfg.DataPlane.Kafka.Brokers) == 0 {
		return fmt.Errorf("consumer dataplane: data_plane.kafka.brokers must not be empty")
	}

	store, p := adapternats.NewRuntimeStore(ctx, cfg.JetStream.URL, cfg.DataPlane.StateBucket)
	if p != nil {
		return fmt.Errorf("consumer dataplane: runtime store init failed: %v", p)
	}
	defer store.Close()

	publisher, p := adapterjs.NewPublisher(context.Background(), adapterjs.PublisherConfig{
		URL:            cfg.JetStream.URL,
		StreamName:     cfg.JetStream.StreamName,
		DedupWindow:    cfg.JetStream.DedupWindowDuration(),
		MaxAge:         cfg.JetStream.MaxAgeDuration(),
		MaxBytes:       cfg.JetStream.MaxBytesInt64(),
		PublishTimeout: 5 * time.Second,
	}, nil)
	if p != nil {
		return fmt.Errorf("consumer dataplane: publisher init failed: %v", p)
	}
	defer publisher.Close(context.Background())

	var ready atomic.Bool
	srv := newDataPlaneHealthServer(cfg.HTTP.Addr, &ready)
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	runtimeState := runtimebootstrap.New(store)
	registry, p := waitForBindingRegistry(ctx, runtimeState, logger)
	if p != nil {
		return fmt.Errorf("consumer dataplane: binding bootstrap failed: %v", p)
	}
	reader, p := adapterkafka.NewReader(adapterkafka.ReaderConfig{
		Brokers: cfg.DataPlane.Kafka.Brokers,
		GroupID: cfg.DataPlane.Kafka.ConsumerGroup,
		Topics:  registry.Topics(),
	})
	if p != nil {
		return fmt.Errorf("consumer dataplane: kafka reader init failed: %v", p)
	}
	defer reader.Close()

	consumeErr := make(chan error, 1)
	ready.Store(true)
	go func() {
		for {
			msg, p := reader.Fetch(ctx)
			if p != nil {
				if ctx.Err() != nil {
					consumeErr <- nil
					return
				}
				consumeErr <- fmt.Errorf("kafka fetch failed: %v", p)
				return
			}

			dataMsg, p := mapKafkaMessage(registry, msg)
			if p != nil {
				logger.Warn("consumer dataplane: kafka boundary rejected message", "topic", msg.Topic, "err", p)
				_ = reader.Commit(ctx, msg)
				continue
			}
			if p := publishCanonicalMessage(ctx, publisher, dataMsg); p != nil {
				logger.Warn("consumer dataplane: publish failed", "message_id", dataMsg.MessageID, "err", p)
				if !p.Retryable {
					_ = reader.Commit(ctx, msg)
				}
				continue
			}
			if p := reader.Commit(ctx, msg); p != nil {
				logger.Warn("consumer dataplane: commit failed", "message_id", dataMsg.MessageID, "err", p)
				continue
			}
			logger.Info("consumer dataplane: published canonical message",
				"binding", dataMsg.Binding,
				"message_id", dataMsg.MessageID,
				"topic", dataMsg.Topic,
			)
		}
	}()

	quit := bootstrap.SignalChannel()
	select {
	case err := <-serverErr:
		return fmt.Errorf("consumer dataplane: http server failed: %w", err)
	case err := <-consumeErr:
		if err != nil {
			return err
		}
	case <-quit:
	case <-ctx.Done():
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeoutDuration())
	defer shutCancel()
	return srv.Shutdown(shutCtx)
}

func waitForBindingRegistry(
	ctx context.Context,
	runtimeState *runtimebootstrap.Runtime,
	logger *slog.Logger,
) (runtimebootstrap.BindingRegistry, *problem.Problem) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		registry, p := runtimeState.BindingRegistry(ctx)
		if p != nil {
			return runtimebootstrap.BindingRegistry{}, p
		}
		if len(registry.Topics()) > 0 {
			return registry, nil
		}
		logger.Info("consumer dataplane: waiting for runtime bindings")
		select {
		case <-ctx.Done():
			return runtimebootstrap.BindingRegistry{}, problem.Wrap(ctx.Err(), problem.Unavailable, "waiting for bindings canceled")
		case <-ticker.C:
		}
	}
}

func mapKafkaMessage(registry runtimebootstrap.BindingRegistry, msg adapterkafka.Message) (dataplane.Message, *problem.Problem) {
	binding, p := registry.BindingForKafkaTopic(msg.Topic)
	if p != nil {
		return dataplane.Message{}, p
	}
	return adapterkafka.MapToDataPlaneMessage(binding, msg)
}

func publishCanonicalMessage(ctx context.Context, publisher dataPlaneEnvelopePublisher, msg dataplane.Message) *problem.Problem {
	env, p := dataplane.NewMessageEnvelope(msg)
	if p != nil {
		return p
	}
	return publisher.Publish(ctx, env)
}

func newDataPlaneHealthServer(addr string, ready *atomic.Bool) *http.Server {
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
