package kafka

import (
	"testing"
	"time"

	"github.com/FabioCaffarello/stream-analytics/internal/application/dataplane"
)

func TestMapToDataPlaneMessage(t *testing.T) {
	msg, p := MapToDataPlaneMessage(
		dataplane.Binding{Name: "orders", KafkaTopic: "mr.orders"},
		Message{
			Topic: "mr.orders",
			Value: []byte(`{"account_id":"a-1"}`),
			Headers: map[string]string{
				"message_id":     "msg-1",
				"correlation_id": "corr-1",
			},
			Time: time.UnixMilli(42),
		},
	)
	if p != nil {
		t.Fatalf("map to dataplane: %v", p)
	}
	if msg.Binding != "orders" {
		t.Fatalf("binding=%q want=orders", msg.Binding)
	}
	if msg.MessageID != "msg-1" {
		t.Fatalf("message_id=%q want=msg-1", msg.MessageID)
	}
}

func TestMapToDataPlaneMessage_RejectsNonObjectJSON(t *testing.T) {
	if _, p := MapToDataPlaneMessage(
		dataplane.Binding{Name: "orders", KafkaTopic: "mr.orders"},
		Message{Topic: "mr.orders", Value: []byte(`[]`)},
	); p == nil {
		t.Fatal("expected validation failure for non-object payload")
	}
}
