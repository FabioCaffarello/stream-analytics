package dataplane

import (
	"context"
	"encoding/json"
	"testing"
)

func TestBindingValidate(t *testing.T) {
	if p := (Binding{}).Validate(); p == nil {
		t.Fatal("expected validation error for empty binding")
	}
	if p := (Binding{Name: "orders", KafkaTopic: "mr.orders"}).Validate(); p != nil {
		t.Fatalf("unexpected validation error: %v", p)
	}
}

func TestConfigValidate(t *testing.T) {
	cfg := Config{
		Name:    "orders-required",
		Binding: "orders",
		Version: "v1",
		Rules:   []Rule{{Field: "account_id", Kind: RuleRequired}},
	}
	if p := cfg.Validate(); p != nil {
		t.Fatalf("unexpected validation error: %v", p)
	}
}

func TestDecodePayloadObject_RequiresJSONObject(t *testing.T) {
	if _, p := DecodePayloadObject([]byte(`[]`)); p == nil {
		t.Fatal("expected validation error for non-object JSON")
	}
	payload, p := DecodePayloadObject([]byte(`{"account_id":"a-1"}`))
	if p != nil {
		t.Fatalf("unexpected decode error: %v", p)
	}
	if got := payload["account_id"]; got != "a-1" {
		t.Fatalf("account_id=%v want=a-1", got)
	}
}

func TestMessageEnvelopeRoundTrip(t *testing.T) {
	msg := Message{
		MessageID:  "msg-1",
		Binding:    "orders",
		Topic:      "mr.orders",
		ProducedAt: 1710000000000,
		Payload:    json.RawMessage(`{"account_id":"a-1"}`),
	}
	env, p := NewMessageEnvelope(msg)
	if p != nil {
		t.Fatalf("new message envelope: %v", p)
	}
	got, p := MessageFromEnvelope(env)
	if p != nil {
		t.Fatalf("message from envelope: %v", p)
	}
	if got.MessageID != msg.MessageID {
		t.Fatalf("message_id=%q want=%q", got.MessageID, msg.MessageID)
	}
}

func TestValidationResultStore_ListNewestFirst(t *testing.T) {
	store := NewMemoryResultStore(2)
	first := ValidationResult{
		MessageID:   "msg-1",
		Binding:     "orders",
		Topic:       "mr.orders",
		Status:      ResultStatusPassed,
		ProcessedAt: 1,
	}
	second := ValidationResult{
		MessageID:   "msg-2",
		Binding:     "orders",
		Topic:       "mr.orders",
		Status:      ResultStatusFailed,
		ProcessedAt: 2,
	}
	third := ValidationResult{
		MessageID:   "msg-3",
		Binding:     "orders",
		Topic:       "mr.orders",
		Status:      ResultStatusPassed,
		ProcessedAt: 3,
	}
	if p := store.Save(first); p != nil {
		t.Fatalf("save first: %v", p)
	}
	if p := store.Save(second); p != nil {
		t.Fatalf("save second: %v", p)
	}
	if p := store.Save(third); p != nil {
		t.Fatalf("save third: %v", p)
	}

	results, p := store.List(context.Background(), ResultQuery{Limit: 10})
	if p != nil {
		t.Fatalf("list results: %v", p)
	}
	if len(results) != 2 {
		t.Fatalf("len(results)=%d want=2", len(results))
	}
	if results[0].MessageID != "msg-3" || results[1].MessageID != "msg-2" {
		t.Fatalf("unexpected order: %+v", results)
	}
	if _, p := store.Get(context.Background(), "msg-1"); p == nil {
		t.Fatal("expected oldest result to be evicted")
	}
}

func TestValidationResultStore_ListFiltersCorrelationID(t *testing.T) {
	store := NewMemoryResultStore(4)
	if p := store.Save(ValidationResult{
		MessageID:     "msg-1",
		Binding:       "orders",
		Topic:         "mr.orders",
		CorrelationID: "corr-a",
		Status:        ResultStatusPassed,
		ProcessedAt:   1,
	}); p != nil {
		t.Fatalf("save first: %v", p)
	}
	if p := store.Save(ValidationResult{
		MessageID:     "msg-2",
		Binding:       "orders",
		Topic:         "mr.orders",
		CorrelationID: "corr-b",
		Status:        ResultStatusFailed,
		ProcessedAt:   2,
	}); p != nil {
		t.Fatalf("save second: %v", p)
	}

	results, p := store.List(context.Background(), ResultQuery{CorrelationID: "corr-a", Limit: 10})
	if p != nil {
		t.Fatalf("list by correlation_id: %v", p)
	}
	if len(results) != 1 || results[0].MessageID != "msg-1" {
		t.Fatalf("results=%+v want msg-1 only", results)
	}
}
