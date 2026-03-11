package store

import (
	"fmt"
	"log/slog"
	"marketmonkey/event"
	"time"

	"marketmonkey/pkg/db"
	"marketmonkey/pkg/metrics"
	"marketmonkey/pkg/nats"

	"github.com/anthdm/hollywood/actor"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

func streams(s *Store) []event.Registry {
	return []event.Registry{
		{
			Stream: nats.StreamTypeStoreCandle,
			Fn:     event.CreateStreamHandler(s.handleCandles),
		},
		{
			Stream: nats.StreamTypeStoreHeatmap,
			Fn:     event.CreateStreamHandler(s.handleHeatmaps),
		},
		{
			Stream: nats.StreamTypeStoreVolume,
			Fn:     event.CreateStreamHandler(s.handleVolumes),
		},
		{
			Stream: nats.StreamTypeStoreStat,
			Fn:     event.CreateStreamHandler(s.handleStats),
		},
	}
}

type Store struct {
	client   db.Client
	consumer *nats.NatsConsumer
	ctx      *actor.Context
	quitch   chan struct{}

	metrics *metrics.MetricsServer
}

func New(client db.Client) actor.Producer {
	return func() actor.Receiver {
		return &Store{
			client: client,
			quitch: make(chan struct{}),
		}
	}
}

func (s *Store) Receive(c *actor.Context) {
	st := time.Now()
	switch msg := c.Message().(type) {

	case actor.Started:
		s.ctx = c
		s.setupMetrics()
		s.consumer = nats.NewNatsConsumer(s.quitch)
		s.consume()

	case *event.Candles:
		if len(msg.Values) == 0 {
			metrics.ReportStoreInsertError("candles", "empty_candles")
			break
		}
		if err := s.client.InsertCandles(c.Context(), msg.Pair, msg.Values); err != nil {
			metrics.ReportStoreInsertError("candles", "insert_error")
			break
		}
		metrics.ReportStoreInsertion("candles", msg.Pair.Exchange, msg.Pair.Symbol, st)
	case *event.Heatmaps:
		// length is always 1
		for _, hm := range msg.Values {
			if err := s.client.InsertHeatmap(c.Context(), hm); err != nil {
				metrics.ReportStoreInsertError("heatmaps", "insert_error")
			}
		}
		metrics.ReportStoreInsertion("heatmaps", msg.Pair.Exchange, msg.Pair.Symbol, st)
	case *event.Volumes:
		if err := s.client.InsertVolumes(c.Context(), msg.Values); err != nil {
			metrics.ReportStoreInsertError("volumes", "insert_error")
		}
		metrics.ReportStoreInsertion("volumes", msg.Pair.Exchange, msg.Pair.Symbol, st)
	case *event.Stats:
		if err := s.client.InsertStats(c.Context(), msg); err != nil {
			metrics.ReportStoreInsertError("stats", "insert_error")
		}
		metrics.ReportStoreInsertion("stats", msg.Pair.Exchange, msg.Pair.Symbol, st)

	case actor.Stopped:
		s.consumer.Close()
		close(s.quitch)
	}
}

func (s *Store) setupMetrics() {
	serviceID := fmt.Sprintf("store-%s", uuid.NewString())
	ms, err := metrics.NewMetricsServer(metrics.Config{
		Tags:      []string{"store"},
		ServiceID: serviceID,
	}, s.quitch)
	if err != nil {
		slog.Error("failed to create metrics server", "error", err)
	}
	s.metrics = ms

	if err := s.metrics.Start(); err != nil {
		slog.Error("failed to start metrics server", "error", err)
	}

	s.metrics.RegisterAll(metrics.StoreMetrics...)

}

func (s *Store) consume() {
	for _, reg := range streams(s) {
		subject := nats.Subject{StreamType: reg.Stream}
		_, err := s.consumer.NewConsumer(nats.ConsumerParams{
			Subject: subject,
			Durable: reg.Stream.Durable("store"),
			Handler: reg.Fn,
		})
		if err != nil {
			slog.Error("failed to create consumer", "stream", reg.Stream, "subject", subject, "err", err)
			panic(err)
		}
	}
}

func (s *Store) handleCandles(candles *event.Candles, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), candles)
	return nil
}

func (s *Store) handleHeatmaps(heatmaps *event.Heatmaps, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), heatmaps)
	return nil
}

func (s *Store) handleVolumes(volumes *event.Volumes, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), volumes)
	return nil
}

func (s *Store) handleStats(stats *event.Stats, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), stats)
	return nil
}
