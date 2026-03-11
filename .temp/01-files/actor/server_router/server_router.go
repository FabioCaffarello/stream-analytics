package server_router

import (
	"log/slog"
	"marketmonkey/event"
	"marketmonkey/pkg/metrics"
	"marketmonkey/pkg/nats"
	"reflect"

	"github.com/anthdm/hollywood/actor"
	"github.com/nats-io/nats.go/jetstream"
)

type StreamMeta struct {
	Name string
	Pids *actor.PIDSet
}

type ServerRouter struct {
	ctx      *actor.Context
	consumer *nats.NatsConsumer
	quitch   chan struct{}

	// active subscriptions from clients
	subscriptions map[nats.Subject]*StreamMeta
}

func New() actor.Producer {
	return func() actor.Receiver {
		return &ServerRouter{
			quitch:        make(chan struct{}),
			subscriptions: make(map[nats.Subject]*StreamMeta),
		}
	}
}

func (s *ServerRouter) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case actor.Started:
		s.ctx = c
		s.consumer = nats.NewNatsConsumer(s.quitch)
		err := s.consumer.Connect()
		if err != nil {
			slog.Error("failed to connect to nats", "err", err)
			panic(err)
		}

	case *event.Subscribe:
		s.handleSubscribe(msg)
	case *event.Unsubscribe:
		s.handleUnsubscribe(msg)

	case *event.Trade:
		s.broadcast(nats.StreamTypeTrade, msg)
	case *event.Orderbook:
		s.broadcast(nats.StreamTypeRealTimeOrderbook, msg)
	case *event.Heatmaps:
		s.broadcast(nats.StreamTypeRealTimeHeatmap, msg)
	case *event.Candles:
		s.broadcast(nats.StreamTypeRealTimeCandle, msg)
	case *event.Stats:
		s.broadcast(nats.StreamTypeRealTimeStat, msg)
	case *event.LiquidationUpdate:
		s.broadcast(nats.StreamTypeLiquidation, msg)
	case *event.Volumes:
		s.broadcast(nats.StreamTypeRealTimeVolume, msg)

	case actor.Stopped:
		if s.consumer != nil {
			s.consumer.Close()
		}
		close(s.quitch)

	default:
		slog.Error("unknown message", "msg", msg, "type", reflect.TypeOf(msg))
	}
}

func (s *ServerRouter) broadcast(streamType nats.StreamType, msg event.StreamData) {
	subject := nats.Subject{
		StreamType: streamType,
		Exchange:   msg.GetPair().Exchange,
		Symbol:     msg.GetPair().Symbol,
		Timeframe:  msg.GetTimeframe(),
	}

	subs, ok := s.subscriptions[subject]
	if ok {
		subs.Pids.ForEach(func(i int, pid *actor.PID) {
			s.ctx.Send(pid, msg)
		})
	}
}

func (s *ServerRouter) handleSubscribe(req *event.Subscribe) {
	slog.Debug("subscribe", "topic", req.Subject.SubString(), "req", req)
	subject := req.Subject

	// check if we already have a consumer for this subject
	if _, ok := s.subscriptions[subject]; !ok {
		// no consumer already, so we create one
		err := s.createConsumer(subject)
		// if it fails, the consumer is not added to the subscriptions map
		// and the subscription is not registered
		if err != nil {
			slog.Error("failed to create consumer", "err", err)
			return
		}
	}

	//check if pid is already in the subscription
	if s.subscriptions[subject].Pids.Contains(req.PID) {
		slog.Debug("pid already in the subscription", "pid", req.PID, "topic", subject)
		return
	}

	// add the subscriber to the topic
	s.subscriptions[subject].Pids.Add(req.PID)

	// update the metrics
	metrics.SetServerSubscriptionCount(
		subject.StreamType.Name(),
		subject.Exchange,
		subject.Symbol,
		s.subscriptions[subject].Pids.Len(),
	)
}
func (s *ServerRouter) handleUnsubscribe(req *event.Unsubscribe) {
	slog.Info("unsubscribe", "topic", req.Subject.SubString())
	subject := req.Subject

	// check the subscription exists
	if s.subscriptions[subject] == nil {
		return
	}

	// remove the pid from the subscription
	s.subscriptions[subject].Pids.Remove(req.PID)

	subCount := s.subscriptions[subject].Pids.Len()

	// if the stream counter is 0, remove the stream
	if subCount == 0 {
		slog.Info("removing nats consumer", "stream", subject.SubString())
		s.consumer.RemoveConsumer(s.subscriptions[subject].Name)
		delete(s.subscriptions, subject)
	}

	// update the metrics
	metrics.SetServerSubscriptionCount(
		subject.StreamType.Name(),
		subject.Exchange,
		subject.Symbol,
		subCount,
	)
}

func (s *ServerRouter) createConsumer(subject nats.Subject) error {
	slog.Info("creating nats consumer", "subject", subject.SubString())
	name, err := s.consumer.NewConsumer(nats.ConsumerParams{
		Subject: subject,
		Handler: s.createMessageHandler(subject.StreamType),
	})
	if err != nil {
		//TODO figure how to handle this error
		slog.Error("failed to create consumer", "err", err)
		return err
	}

	s.subscriptions[subject] = &StreamMeta{
		Name: name,
		Pids: actor.NewPIDSet(),
	}

	return nil
}

func (s *ServerRouter) createMessageHandler(streamType nats.StreamType) func([]byte, *jetstream.MsgMetadata) error {
	switch streamType {
	case nats.StreamTypeRealTimeCandle:
		return event.CreateStreamHandler(s.handleCandles)
	case nats.StreamTypeRealTimeStat:
		return event.CreateStreamHandler(s.handleStats)
	case nats.StreamTypeRealTimeVolume:
		return event.CreateStreamHandler(s.handleVolumes)
	case nats.StreamTypeRealTimeHeatmap:
		return event.CreateStreamHandler(s.handleHeatmaps)
	case nats.StreamTypeTrade:
		return event.CreateStreamHandler(s.handleTrades)
	case nats.StreamTypeBookUpdate:
		return event.CreateStreamHandler(s.handleOrderbook)
	case nats.StreamTypeLiquidation:
		return event.CreateStreamHandler(s.handleLiquidations)
	case nats.StreamTypeRealTimeOrderbook:
		return event.CreateStreamHandler(s.handleOrderbook)
	default:
		slog.Error("unknown stream type", "streamType", streamType)
		return func(msg []byte, meta *jetstream.MsgMetadata) error {
			return nil
		}
	}
}

func (s *ServerRouter) handleCandles(candles *event.Candles, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), candles)
	return nil
}

func (s *ServerRouter) handleHeatmaps(heatmap *event.Heatmaps, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), heatmap)
	return nil
}

func (s *ServerRouter) handleVolumes(volumes *event.Volumes, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), volumes)
	return nil
}

func (s *ServerRouter) handleStats(stats *event.Stats, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), stats)
	return nil
}

func (s *ServerRouter) handleTrades(trades *event.Trade, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), trades)
	return nil
}

func (s *ServerRouter) handleOrderbook(orderbook *event.Orderbook, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), orderbook)
	return nil
}

func (s *ServerRouter) handleLiquidations(liquidations *event.LiquidationUpdate, _ *jetstream.MsgMetadata) error {
	s.ctx.Send(s.ctx.PID(), liquidations)
	return nil
}
