package validatorruntime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/application/dataplane"
	"github.com/FabioCaffarello/stream-analytics/internal/application/runtimebootstrap"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type fakeClock struct{ now int64 }

func (f fakeClock) NowUnixMilli() int64 { return f.now }

type fakePublisher struct {
	published []envelope.Envelope
}

func (p *fakePublisher) Publish(_ context.Context, env envelope.Envelope) *problem.Problem {
	p.published = append(p.published, env)
	return nil
}

type fakeResultStore struct {
	saved []dataplane.ValidationResult
}

func (s *fakeResultStore) Save(result dataplane.ValidationResult) *problem.Problem {
	s.saved = append(s.saved, result)
	return nil
}

func (s *fakeResultStore) List(context.Context, dataplane.ResultQuery) ([]dataplane.ValidationResult, *problem.Problem) {
	return append([]dataplane.ValidationResult(nil), s.saved...), nil
}

func (s *fakeResultStore) Get(_ context.Context, messageID string) (dataplane.ValidationResult, *problem.Problem) {
	for _, result := range s.saved {
		if result.MessageID == messageID {
			return result, nil
		}
	}
	return dataplane.ValidationResult{}, problem.New(problem.NotFound, "validation result not found")
}

type testStore struct {
	active map[string]dataplane.Config
}

func (s testStore) UpsertBinding(context.Context, dataplane.Binding) *problem.Problem { return nil }
func (s testStore) ListBindings(context.Context) ([]dataplane.Binding, *problem.Problem) {
	return []dataplane.Binding{{Name: "orders", KafkaTopic: "mr.orders"}}, nil
}
func (s testStore) UpsertConfig(context.Context, dataplane.Config) *problem.Problem { return nil }
func (s testStore) ListConfigs(context.Context, string) ([]dataplane.Config, *problem.Problem) {
	return nil, nil
}
func (s testStore) ActivateConfig(context.Context, dataplane.Activation) *problem.Problem { return nil }
func (s testStore) ActiveConfig(_ context.Context, binding string) (*dataplane.Config, *problem.Problem) {
	cfg, ok := s.active[binding]
	if !ok {
		return nil, problem.New(problem.NotFound, "active config not found")
	}
	return &cfg, nil
}

func TestProcessorPassesValidPayload(t *testing.T) {
	pub := &fakePublisher{}
	results := &fakeResultStore{}
	rt := runtimebootstrap.New(testStore{active: map[string]dataplane.Config{
		"orders": {
			Name:    "orders-v1",
			Binding: "orders",
			Version: "v1",
			Rules: []dataplane.Rule{
				{Field: "account_id", Kind: dataplane.RuleRequired},
				{Field: "account_id", Kind: dataplane.RuleNotEmpty},
				{Field: "status", Kind: dataplane.RuleEquals, Expected: "OPEN"},
			},
		},
	}})
	processor := NewProcessor(rt, fakeClock{now: 50}, pub, results)

	result, p := processor.Process(context.Background(), dataplane.Message{
		MessageID:  "msg-1",
		Binding:    "orders",
		Topic:      "mr.orders",
		ProducedAt: 10,
		Payload:    json.RawMessage(`{"account_id":"a-1","status":"OPEN"}`),
	})
	if p != nil {
		t.Fatalf("process: %v", p)
	}
	if result.Status != dataplane.ResultStatusPassed {
		t.Fatalf("status=%q want=passed", result.Status)
	}
	if len(pub.published) != 1 {
		t.Fatalf("published=%d want=1", len(pub.published))
	}
	if len(results.saved) != 1 {
		t.Fatalf("saved results=%d want=1", len(results.saved))
	}
}

func TestProcessorFailsMissingRequired(t *testing.T) {
	pub := &fakePublisher{}
	results := &fakeResultStore{}
	rt := runtimebootstrap.New(testStore{active: map[string]dataplane.Config{
		"orders": {
			Name:    "orders-v1",
			Binding: "orders",
			Version: "v1",
			Rules:   []dataplane.Rule{{Field: "account_id", Kind: dataplane.RuleRequired}},
		},
	}})
	processor := NewProcessor(rt, fakeClock{now: 50}, pub, results)

	result, p := processor.Process(context.Background(), dataplane.Message{
		MessageID:  "msg-1",
		Binding:    "orders",
		Topic:      "mr.orders",
		ProducedAt: 10,
		Payload:    json.RawMessage(`{"status":"OPEN"}`),
	})
	if p != nil {
		t.Fatalf("process: %v", p)
	}
	if result.Status != dataplane.ResultStatusFailed {
		t.Fatalf("status=%q want=failed", result.Status)
	}
	if len(result.Violations) != 1 {
		t.Fatalf("violations=%d want=1", len(result.Violations))
	}
	if len(results.saved) != 1 {
		t.Fatalf("saved results=%d want=1", len(results.saved))
	}
}

func TestProcessorPublishesErrorWhenActiveConfigMissing(t *testing.T) {
	pub := &fakePublisher{}
	results := &fakeResultStore{}
	processor := NewProcessor(runtimebootstrap.New(testStore{}), fakeClock{now: 75}, pub, results)

	result, p := processor.Process(context.Background(), dataplane.Message{
		MessageID:  "msg-err",
		Binding:    "orders",
		Topic:      "mr.orders",
		ProducedAt: 10,
		Payload:    json.RawMessage(`{"status":"OPEN"}`),
	})
	if p != nil {
		t.Fatalf("process: %v", p)
	}
	if result.Status != dataplane.ResultStatusError {
		t.Fatalf("status=%q want=error", result.Status)
	}
	if len(results.saved) != 1 {
		t.Fatalf("saved results=%d want=1", len(results.saved))
	}
}
