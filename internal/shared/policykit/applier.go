package policykit

import (
	"sync"

	"github.com/market-raccoon/internal/shared/envelope"
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

// Apply executes Drop/Degrade/Compress actions in deterministic order.
func (a *Applier) Apply(decision Decision, envs []envelope.Envelope, hooks ApplyHooks) []envelope.Envelope {
	if len(envs) == 0 {
		return nil
	}
	resolver := a.resolveResolver(hooks)
	partitionFn := resolvePartitionKey(hooks)
	stride, strideEnabled := resolveStride(decision)

	out := make([]envelope.Envelope, 0, len(envs))
	for _, env := range envs {
		category := resolver.ResolveSubject(envelope.SubjectFromEnvelope(env))
		if shouldDrop(decision, category) {
			continue
		}

		count := a.nextCount(partitionFn(env))
		if strideEnabled && category != CategoryCloseFinal && count%stride != 0 {
			continue
		}

		if decision.HasAction(ActionCompressSnapshot) && category == CategorySnapshot && hooks.CompressSnapshot != nil {
			if compressed, ok := hooks.CompressSnapshot(env); ok {
				env = compressed
			}
		}
		out = append(out, env)
	}
	return out
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
	return decision.HasAction(ActionDropDelta) && category == CategoryDelta && !NeverDropCloseFinal(category, decision)
}
