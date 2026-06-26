package deliveryruntime

import (
	"io"
	"log/slog"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/ids"
	"github.com/anthdm/hollywood/actor"
)

type benchDeliverySink struct {
	ch chan DeliveryEvent
}

func (s *benchDeliverySink) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case DeliveryEvent:
		s.ch <- msg
	}
}

var benchDeliveryFanOutSink int64

func BenchmarkDeliveryFanOut_50Sessions(b *testing.B) {
	benchmarkDeliveryFanOut(b, 50)
}

func BenchmarkDeliveryFanOut_200Sessions(b *testing.B) {
	benchmarkDeliveryFanOut(b, 200)
}

func benchmarkDeliveryFanOut(b *testing.B, sessions int) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		b.Fatalf("new engine: %v", err)
	}

	routerPID := e.Spawn(NewRouterActor(RouterConfig{
		Timeframe: "raw",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}), "bench-router")
	b.Cleanup(func() {
		if routerPID != nil {
			<-e.Poison(routerPID).Done()
		}
	})

	subject, p := domain.ParseSubject("marketdata.trade/binance/BTC-USDT/raw")
	if p != nil {
		b.Fatalf("parse subject: %v", p)
	}

	type sessionTarget struct {
		pid *actor.PID
		ch  chan DeliveryEvent
	}
	targets := make([]sessionTarget, 0, sessions)

	for i := 0; i < sessions; i++ {
		ch := make(chan DeliveryEvent, 1024)
		pid := e.Spawn(func() actor.Receiver {
			return &benchDeliverySink{ch: ch}
		}, "bench-session")
		targets = append(targets, sessionTarget{pid: pid, ch: ch})
		sessionID := ids.NewSessionID()
		e.Send(routerPID, RegisterSession{SessionID: sessionID, PID: pid})
		e.Send(routerPID, SubscribeSession{SessionID: sessionID, Subject: subject})
	}

	b.Cleanup(func() {
		for _, target := range targets {
			if target.pid != nil {
				<-e.Poison(target.pid).Done()
			}
		}
	})

	env := envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		TsIngest:   1_735_689_600_000,
		Payload:    []byte(`{"price":100.0}`),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env.Seq = int64(i + 1)
		e.Send(routerPID, DeliverEnvelope{Envelope: env})
		for j := 0; j < sessions; j++ {
			msg := <-targets[j].ch
			benchDeliveryFanOutSink += msg.Env.Seq
		}
	}
}
