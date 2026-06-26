package deliveryruntime

import (
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/anthdm/hollywood/actor"
)

func TestDeliverySubsystem_forwardsBusEnvelopeToRouter(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	envCh := make(chan envelope.Envelope, 16)
	routerInbox := make(chan any, 16)
	subsystemPID := e.Spawn(NewSubsystemActor(SubsystemConfig{
		EnvelopeCh: envCh,
		RouterProducer: func() actor.Receiver {
			return &captureActor{ch: routerInbox}
		},
	}), "delivery-subsystem")
	defer e.Poison(subsystemPID)

	env := envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        1,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"ok":true}`),
	}
	envCh <- env

	msg := waitForMessage[DeliverEnvelope](t, routerInbox, time.Second)
	if msg.Envelope.Seq != env.Seq {
		t.Fatalf("forwarded seq=%d want=%d", msg.Envelope.Seq, env.Seq)
	}
}

func TestDeliverySubsystem_stopCancelsConsumeLoop(t *testing.T) {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	envCh := make(chan envelope.Envelope, 16)
	routerInbox := make(chan any, 16)
	subsystemPID := e.Spawn(NewSubsystemActor(SubsystemConfig{
		EnvelopeCh: envCh,
		RouterProducer: func() actor.Receiver {
			return &captureActor{ch: routerInbox}
		},
	}), "delivery-subsystem")

	<-e.Poison(subsystemPID).Done()
	envCh <- envelope.Envelope{
		Type:       "marketdata.trade",
		Version:    1,
		Venue:      "binance",
		Instrument: "BTC-USDT",
		Seq:        2,
		TsIngest:   time.Now().UnixMilli(),
		Payload:    []byte(`{"ok":true}`),
	}

	select {
	case msg := <-routerInbox:
		if _, ok := msg.(DeliverEnvelope); ok {
			t.Fatal("unexpected forwarded envelope after subsystem stop")
		}
	case <-time.After(150 * time.Millisecond):
	}
}
