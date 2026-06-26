package main

import (
	"context"
	"testing"
	"time"

	adapterkafka "github.com/FabioCaffarello/stream-analytics/internal/adapters/kafka"
	"github.com/FabioCaffarello/stream-analytics/internal/application/dataplane"
	"github.com/FabioCaffarello/stream-analytics/internal/application/runtimebootstrap"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type fakeDataPlanePublisher struct {
	envs []envelope.Envelope
}

func (p *fakeDataPlanePublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.envs = append(p.envs, env)
	return nil
}

func TestMapKafkaMessageUsesBindingRegistry(t *testing.T) {
	registry, p := runtimebootstrap.NewBindingRegistry([]dataplane.Binding{{
		Name:       "orders",
		KafkaTopic: "mr.orders",
	}})
	if p != nil {
		t.Fatalf("new binding registry: %v", p)
	}

	msg, p := mapKafkaMessage(registry, adapterkafka.Message{
		Topic: "mr.orders",
		Value: []byte(`{"account_id":"acct-1"}`),
		Headers: map[string]string{
			"message_id": "msg-1",
		},
		Time: time.UnixMilli(42),
	})
	if p != nil {
		t.Fatalf("map kafka message: %v", p)
	}
	if msg.Binding != "orders" {
		t.Fatalf("binding=%q want=orders", msg.Binding)
	}
}

func TestPublishCanonicalMessageBuildsDataplaneEnvelope(t *testing.T) {
	pub := &fakeDataPlanePublisher{}
	p := publishCanonicalMessage(context.Background(), pub, dataplane.Message{
		MessageID:  "msg-1",
		Binding:    "orders",
		Topic:      "mr.orders",
		ProducedAt: 10,
		Payload:    []byte(`{"account_id":"acct-1"}`),
	})
	if p != nil {
		t.Fatalf("publish canonical message: %v", p)
	}
	if len(pub.envs) != 1 {
		t.Fatalf("published envs=%d want=1", len(pub.envs))
	}
	if pub.envs[0].Type != dataplane.EventTypeMessage {
		t.Fatalf("env type=%q want=%q", pub.envs[0].Type, dataplane.EventTypeMessage)
	}
}
