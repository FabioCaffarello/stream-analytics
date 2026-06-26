package runtimebootstrap

import (
	"context"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/application/dataplane"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

type Store interface {
	UpsertBinding(ctx context.Context, binding dataplane.Binding) *problem.Problem
	ListBindings(ctx context.Context) ([]dataplane.Binding, *problem.Problem)
	UpsertConfig(ctx context.Context, cfg dataplane.Config) *problem.Problem
	ListConfigs(ctx context.Context, binding string) ([]dataplane.Config, *problem.Problem)
	ActivateConfig(ctx context.Context, activation dataplane.Activation) *problem.Problem
	ActiveConfig(ctx context.Context, binding string) (*dataplane.Config, *problem.Problem)
}

type Snapshot struct {
	Bindings []dataplane.Binding         `json:"bindings"`
	Active   map[string]dataplane.Config `json:"active"`
}

type BindingRegistry struct {
	byName  map[string]dataplane.Binding
	byTopic map[string]dataplane.Binding
	topics  []string
}

func NewBindingRegistry(bindings []dataplane.Binding) (BindingRegistry, *problem.Problem) {
	registry := BindingRegistry{
		byName:  make(map[string]dataplane.Binding, len(bindings)),
		byTopic: make(map[string]dataplane.Binding, len(bindings)),
		topics:  make([]string, 0, len(bindings)),
	}
	for _, binding := range bindings {
		if p := binding.Validate(); p != nil {
			return BindingRegistry{}, p
		}
		if existing, ok := registry.byTopic[binding.KafkaTopic]; ok && existing.Name != binding.Name {
			return BindingRegistry{}, problem.Newf(problem.Conflict, "kafka topic %q is already bound to %q", binding.KafkaTopic, existing.Name)
		}
		registry.byName[binding.Name] = binding
		if _, ok := registry.byTopic[binding.KafkaTopic]; !ok {
			registry.topics = append(registry.topics, binding.KafkaTopic)
		}
		registry.byTopic[binding.KafkaTopic] = binding
	}
	return registry, nil
}

func (r BindingRegistry) Topics() []string {
	return append([]string(nil), r.topics...)
}

func (r BindingRegistry) BindingByName(name string) (dataplane.Binding, *problem.Problem) {
	name = strings.TrimSpace(name)
	if name == "" {
		return dataplane.Binding{}, problem.New(problem.ValidationFailed, "binding name must not be empty")
	}
	binding, ok := r.byName[name]
	if !ok {
		return dataplane.Binding{}, problem.Newf(problem.NotFound, "binding %q not found", name)
	}
	return binding, nil
}

func (r BindingRegistry) BindingForKafkaTopic(topic string) (dataplane.Binding, *problem.Problem) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return dataplane.Binding{}, problem.New(problem.ValidationFailed, "kafka topic must not be empty")
	}
	binding, ok := r.byTopic[topic]
	if !ok {
		return dataplane.Binding{}, problem.Newf(problem.NotFound, "binding for kafka topic %q not found", topic)
	}
	return binding, nil
}

type Runtime struct {
	store Store
}

func New(store Store) *Runtime {
	return &Runtime{store: store}
}

func (r *Runtime) UpsertBinding(ctx context.Context, binding dataplane.Binding) *problem.Problem {
	if r == nil || r.store == nil {
		return problem.New(problem.Internal, "runtime store must not be nil")
	}
	if p := binding.Validate(); p != nil {
		return p
	}
	bindings, p := r.store.ListBindings(ctx)
	if p != nil {
		return p
	}
	for _, existing := range bindings {
		if existing.Name == binding.Name {
			continue
		}
		if strings.EqualFold(existing.KafkaTopic, binding.KafkaTopic) {
			return problem.Newf(problem.Conflict, "kafka topic %q is already bound to %q", binding.KafkaTopic, existing.Name)
		}
	}
	return r.store.UpsertBinding(ctx, binding)
}

func (r *Runtime) Bindings(ctx context.Context) ([]dataplane.Binding, *problem.Problem) {
	if r == nil || r.store == nil {
		return nil, problem.New(problem.Internal, "runtime store must not be nil")
	}
	return r.store.ListBindings(ctx)
}

func (r *Runtime) BindingByName(ctx context.Context, name string) (dataplane.Binding, *problem.Problem) {
	registry, p := r.BindingRegistry(ctx)
	if p != nil {
		return dataplane.Binding{}, p
	}
	return registry.BindingByName(name)
}

func (r *Runtime) BindingForKafkaTopic(ctx context.Context, topic string) (dataplane.Binding, *problem.Problem) {
	registry, p := r.BindingRegistry(ctx)
	if p != nil {
		return dataplane.Binding{}, p
	}
	return registry.BindingForKafkaTopic(topic)
}

func (r *Runtime) BindingRegistry(ctx context.Context) (BindingRegistry, *problem.Problem) {
	bindings, p := r.Bindings(ctx)
	if p != nil {
		return BindingRegistry{}, p
	}
	return NewBindingRegistry(bindings)
}

func (r *Runtime) UpsertConfig(ctx context.Context, cfg dataplane.Config) *problem.Problem {
	if r == nil || r.store == nil {
		return problem.New(problem.Internal, "runtime store must not be nil")
	}
	if p := cfg.Validate(); p != nil {
		return p
	}
	if _, p := r.BindingByName(ctx, cfg.Binding); p != nil {
		return p
	}
	return r.store.UpsertConfig(ctx, cfg)
}

func (r *Runtime) Configs(ctx context.Context, binding string) ([]dataplane.Config, *problem.Problem) {
	if r == nil || r.store == nil {
		return nil, problem.New(problem.Internal, "runtime store must not be nil")
	}
	return r.store.ListConfigs(ctx, binding)
}

func (r *Runtime) ActivateConfig(ctx context.Context, activation dataplane.Activation) *problem.Problem {
	if r == nil || r.store == nil {
		return problem.New(problem.Internal, "runtime store must not be nil")
	}
	if p := activation.Validate(); p != nil {
		return p
	}
	configs, p := r.store.ListConfigs(ctx, activation.Binding)
	if p != nil {
		return p
	}
	for _, cfg := range configs {
		if cfg.Version == activation.Version {
			return r.store.ActivateConfig(ctx, activation)
		}
	}
	return problem.Newf(problem.NotFound, "config %q for binding %q not found", activation.Version, activation.Binding)
}

func (r *Runtime) ActiveConfig(ctx context.Context, binding string) (*dataplane.Config, *problem.Problem) {
	if r == nil || r.store == nil {
		return nil, problem.New(problem.Internal, "runtime store must not be nil")
	}
	return r.store.ActiveConfig(ctx, binding)
}

func (r *Runtime) Snapshot(ctx context.Context) (Snapshot, *problem.Problem) {
	bindings, p := r.Bindings(ctx)
	if p != nil {
		return Snapshot{}, p
	}
	snapshot := Snapshot{
		Bindings: bindings,
		Active:   make(map[string]dataplane.Config, len(bindings)),
	}
	for _, binding := range bindings {
		cfg, p := r.ActiveConfig(ctx, binding.Name)
		if p != nil {
			if p.Code == problem.NotFound {
				continue
			}
			return Snapshot{}, p
		}
		if cfg != nil {
			snapshot.Active[binding.Name] = *cfg
		}
	}
	return snapshot, nil
}
