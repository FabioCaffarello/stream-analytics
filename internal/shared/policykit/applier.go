package policykit

import (
	"sync"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
)

// ApplyHooks customizes generic applier behavior.
type ApplyHooks struct {
	Resolver CategoryResolver

	// PartitionKey defines counter partitioning for stride degradation.
	// Default: event_type + venue + instrument.
	PartitionKey func(env envelope.Envelope) string

	// CompressSnapshot optionally rewrites snapshot envelopes.
	CompressSnapshot func(env envelope.Envelope) (envelope.Envelope, bool)
}

// Applier applies deterministic policy decisions over envelope lists.
type Applier struct {
	mu       sync.Mutex
	counters map[string]uint64
	resolver CategoryResolver
}

func NewApplier(resolver CategoryResolver) *Applier {
	return &Applier{
		counters: make(map[string]uint64),
		resolver: resolver,
	}
}

// ApplySingle executes Drop/Degrade/Compress actions for one envelope.
func (a *Applier) ApplySingle(decision Decision, env envelope.Envelope, hooks ApplyHooks) (envelope.Envelope, bool) {
	resolver := a.resolveResolver(hooks)
	partitionFn := resolvePartitionKey(hooks)
	stride, strideEnabled := resolveStride(decision)
	return a.applySingleResolved(decision, env, hooks, resolver, partitionFn, stride, strideEnabled)
}

// Apply executes Drop/Degrade/Compress actions in deterministic order.
func (a *Applier) Apply(decision Decision, envs []envelope.Envelope, hooks ApplyHooks) []envelope.Envelope {
	if len(envs) == 0 {
		return nil
	}
	resolver := a.resolveResolver(hooks)
	partitionFn := resolvePartitionKey(hooks)
	stride, strideEnabled := resolveStride(decision)
	if len(envs) == 1 {
		if envOut, keep := a.applySingleResolved(decision, envs[0], hooks, resolver, partitionFn, stride, strideEnabled); keep {
			return []envelope.Envelope{envOut}
		}
		return nil
	}

	out := make([]envelope.Envelope, 0, len(envs))
	for _, env := range envs {
		if envOut, keep := a.applySingleResolved(decision, env, hooks, resolver, partitionFn, stride, strideEnabled); keep {
			out = append(out, envOut)
		}
	}
	return out
}

func (a *Applier) applySingleResolved(
	decision Decision,
	env envelope.Envelope,
	hooks ApplyHooks,
	resolver CategoryResolver,
	partitionFn func(envelope.Envelope) string,
	stride uint64,
	strideEnabled bool,
) (envelope.Envelope, bool) {
	category := resolver.ResolveSubject(envelope.SubjectFromEnvelope(env))
	if shouldDrop(decision, category) {
		return env, false
	}

	if strideEnabled && category != CategoryCloseFinal {
		count := a.nextCount(partitionFn(env))
		if count%stride != 0 {
			return env, false
		}
	}

	if decision.HasAction(ActionCompressSnapshot) && category == CategorySnapshot && hooks.CompressSnapshot != nil {
		if compressed, ok := hooks.CompressSnapshot(env); ok {
			env = compressed
		}
	}
	return env, true
}

func (a *Applier) nextCount(partition string) uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.counters[partition]++
	return a.counters[partition]
}

func (a *Applier) resolveResolver(hooks ApplyHooks) CategoryResolver {
	resolver := hooks.Resolver
	if len(resolver.bySubject) != 0 || len(resolver.byEventType) != 0 {
		return resolver
	}
	if len(a.resolver.bySubject) != 0 || len(a.resolver.byEventType) != 0 {
		return a.resolver
	}
	return NewCategoryResolver()
}

func defaultPartitionKey(env envelope.Envelope) string {
	return env.Type + "|" + env.Venue + "|" + env.Instrument
}

func resolvePartitionKey(hooks ApplyHooks) func(envelope.Envelope) string {
	if hooks.PartitionKey != nil {
		return hooks.PartitionKey
	}
	return defaultPartitionKey
}

func resolveStride(decision Decision) (uint64, bool) {
	stride := decision.DegradeStride()
	if stride <= 1 {
		return 0, false
	}
	return uint64(stride), true
}

func shouldDrop(decision Decision, category Category) bool {
	return DropAllowed(category, decision)
}
