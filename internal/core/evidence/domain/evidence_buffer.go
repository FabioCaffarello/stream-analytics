package domain

import "github.com/FabioCaffarello/stream-analytics/internal/shared/problem"

// EvidenceBufferPolicy defines bounded ring limits for in-memory evidence storage.
type EvidenceBufferPolicy struct {
	MaxPerKind int
}

// NewEvidenceBufferPolicy validates and returns a bounded policy.
func NewEvidenceBufferPolicy(maxPerKind int) (EvidenceBufferPolicy, *problem.Problem) {
	if maxPerKind <= 0 {
		return EvidenceBufferPolicy{}, problem.New(problem.ValidationFailed, "evidence buffer max_per_kind must be > 0")
	}
	return EvidenceBufferPolicy{MaxPerKind: maxPerKind}, nil
}

// EvidenceBuffer is a deterministic per-kind ring buffer.
type EvidenceBuffer struct {
	policy EvidenceBufferPolicy
	rings  map[EvidenceType][]EvidenceEvent
	head   map[EvidenceType]int
}

// NewEvidenceBuffer creates a bounded evidence buffer.
func NewEvidenceBuffer(policy EvidenceBufferPolicy) *EvidenceBuffer {
	return &EvidenceBuffer{
		policy: policy,
		rings:  make(map[EvidenceType][]EvidenceEvent, len(validTypes)),
		head:   make(map[EvidenceType]int, len(validTypes)),
	}
}

// Push appends one evidence event.
// Returns overwritten=true when the oldest item was replaced.
func (b *EvidenceBuffer) Push(ev EvidenceEvent) (overwritten bool, p *problem.Problem) {
	if b == nil {
		return false, problem.New(problem.ValidationFailed, "evidence buffer is nil")
	}
	if p := ev.Validate(); p != nil {
		return false, p
	}
	ring := b.rings[ev.Type]
	if len(ring) < b.policy.MaxPerKind {
		b.rings[ev.Type] = append(ring, ev)
		return false, nil
	}
	if len(ring) == 0 {
		// Defensive fallback when buffer is misconfigured.
		return false, problem.New(problem.ValidationFailed, "evidence buffer ring is empty under full state")
	}
	idx := b.head[ev.Type]
	ring[idx] = ev
	b.head[ev.Type] = (idx + 1) % b.policy.MaxPerKind
	b.rings[ev.Type] = ring
	return true, nil
}

// List returns events in chronological order (oldest -> newest).
func (b *EvidenceBuffer) List(kind EvidenceType) []EvidenceEvent {
	if b == nil {
		return nil
	}
	ring := b.rings[kind]
	if len(ring) == 0 {
		return nil
	}
	if len(ring) < b.policy.MaxPerKind {
		out := make([]EvidenceEvent, len(ring))
		copy(out, ring)
		return out
	}
	out := make([]EvidenceEvent, 0, len(ring))
	start := b.head[kind]
	for i := range len(ring) {
		out = append(out, ring[(start+i)%len(ring)])
	}
	return out
}

// Size returns current number of entries for one kind.
func (b *EvidenceBuffer) Size(kind EvidenceType) int {
	if b == nil {
		return 0
	}
	return len(b.rings[kind])
}

// Total returns current total entries across all kinds.
func (b *EvidenceBuffer) Total() int {
	if b == nil {
		return 0
	}
	total := 0
	for _, ring := range b.rings {
		total += len(ring)
	}
	return total
}
