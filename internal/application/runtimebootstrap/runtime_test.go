package runtimebootstrap

import (
	"context"
	"testing"

	"github.com/FabioCaffarello/stream-analytics/internal/application/dataplane"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type memoryStore struct {
	bindings map[string]dataplane.Binding
	configs  map[string]map[string]dataplane.Config
	active   map[string]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		bindings: make(map[string]dataplane.Binding),
		configs:  make(map[string]map[string]dataplane.Config),
		active:   make(map[string]string),
	}
}

func (s *memoryStore) UpsertBinding(_ context.Context, binding dataplane.Binding) *problem.Problem {
	s.bindings[binding.Name] = binding
	return nil
}

func (s *memoryStore) ListBindings(_ context.Context) ([]dataplane.Binding, *problem.Problem) {
	out := make([]dataplane.Binding, 0, len(s.bindings))
	for _, binding := range s.bindings {
		out = append(out, binding)
	}
	return out, nil
}

func (s *memoryStore) UpsertConfig(_ context.Context, cfg dataplane.Config) *problem.Problem {
	if _, ok := s.configs[cfg.Binding]; !ok {
		s.configs[cfg.Binding] = make(map[string]dataplane.Config)
	}
	s.configs[cfg.Binding][cfg.Version] = cfg
	return nil
}

func (s *memoryStore) ListConfigs(_ context.Context, binding string) ([]dataplane.Config, *problem.Problem) {
	byVersion := s.configs[binding]
	out := make([]dataplane.Config, 0, len(byVersion))
	for _, cfg := range byVersion {
		out = append(out, cfg)
	}
	return out, nil
}

func (s *memoryStore) ActivateConfig(_ context.Context, activation dataplane.Activation) *problem.Problem {
	s.active[activation.Binding] = activation.Version
	return nil
}

func (s *memoryStore) ActiveConfig(_ context.Context, binding string) (*dataplane.Config, *problem.Problem) {
	version, ok := s.active[binding]
	if !ok {
		return nil, problem.New(problem.NotFound, "active config not found")
	}
	cfg := s.configs[binding][version]
	return &cfg, nil
}

func TestRuntimeBindingLookupByTopic(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	rt := New(store)
	if p := rt.UpsertBinding(ctx, dataplane.Binding{Name: "orders", KafkaTopic: "mr.orders"}); p != nil {
		t.Fatalf("upsert binding: %v", p)
	}
	binding, p := rt.BindingForKafkaTopic(ctx, "mr.orders")
	if p != nil {
		t.Fatalf("binding for topic: %v", p)
	}
	if binding.Name != "orders" {
		t.Fatalf("binding name=%q want=orders", binding.Name)
	}
}

func TestRuntimeBuildsBindingRegistry(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	rt := New(store)
	if p := rt.UpsertBinding(ctx, dataplane.Binding{Name: "orders", KafkaTopic: "mr.orders"}); p != nil {
		t.Fatalf("upsert orders binding: %v", p)
	}
	if p := rt.UpsertBinding(ctx, dataplane.Binding{Name: "fills", KafkaTopic: "mr.fills"}); p != nil {
		t.Fatalf("upsert fills binding: %v", p)
	}

	registry, p := rt.BindingRegistry(ctx)
	if p != nil {
		t.Fatalf("binding registry: %v", p)
	}
	if got := registry.Topics(); len(got) != 2 {
		t.Fatalf("topics=%v want=2 topics", got)
	}
	binding, p := registry.BindingByName("fills")
	if p != nil {
		t.Fatalf("binding by name: %v", p)
	}
	if binding.KafkaTopic != "mr.fills" {
		t.Fatalf("fills kafka_topic=%q want=mr.fills", binding.KafkaTopic)
	}
}

func TestRuntimeRejectsDuplicateKafkaTopics(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	rt := New(store)
	if p := rt.UpsertBinding(ctx, dataplane.Binding{Name: "orders", KafkaTopic: "mr.orders"}); p != nil {
		t.Fatalf("upsert first binding: %v", p)
	}
	if p := rt.UpsertBinding(ctx, dataplane.Binding{Name: "audit", KafkaTopic: "mr.orders"}); p == nil {
		t.Fatal("expected duplicate kafka topic conflict")
	}
}

func TestRuntimeActivateConfig(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	rt := New(store)
	if p := rt.UpsertBinding(ctx, dataplane.Binding{Name: "orders", KafkaTopic: "mr.orders"}); p != nil {
		t.Fatalf("upsert binding: %v", p)
	}
	cfg := dataplane.Config{
		Name:    "orders-required",
		Binding: "orders",
		Version: "v1",
		Rules:   []dataplane.Rule{{Field: "account_id", Kind: dataplane.RuleRequired}},
	}
	if p := rt.UpsertConfig(ctx, cfg); p != nil {
		t.Fatalf("upsert config: %v", p)
	}
	if p := rt.ActivateConfig(ctx, dataplane.Activation{Binding: "orders", Version: "v1", ActivatedAt: 10}); p != nil {
		t.Fatalf("activate config: %v", p)
	}
	active, p := rt.ActiveConfig(ctx, "orders")
	if p != nil {
		t.Fatalf("active config: %v", p)
	}
	if active.Version != "v1" {
		t.Fatalf("active version=%q want=v1", active.Version)
	}
}
