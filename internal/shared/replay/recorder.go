package replay

import (
	"context"
	"strings"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// EventPublisher is the minimal publishing contract required by RecorderPublisher.
type EventPublisher interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

// RecorderPublisher wraps an EventPublisher, writing fixtures before publish.
type RecorderPublisher struct {
	inner  EventPublisher
	writer *Writer
}

// NewRecorderPublisher creates an EventPublisher wrapper that records all envelopes to path.
func NewRecorderPublisher(inner EventPublisher, path string, opts ...WriterOption) (*RecorderPublisher, *problem.Problem) {
	if inner == nil {
		return nil, problem.New(problem.ValidationFailed, "recorder publisher inner must not be nil")
	}
	if strings.TrimSpace(path) == "" {
		return nil, problem.WithDetail(
			problem.New(problem.ValidationFailed, "record path must not be empty"),
			"field", "path",
		)
	}
	w, p := NewWriter(path, opts...)
	if p != nil {
		return nil, p
	}
	return &RecorderPublisher{inner: inner, writer: w}, nil
}

// Publish appends to fixture first, then delegates to the wrapped publisher.
func (r *RecorderPublisher) Publish(ctx context.Context, env envelope.Envelope) *problem.Problem {
	if r == nil || r.writer == nil || r.inner == nil {
		return problem.New(problem.ValidationFailed, "recorder publisher is not initialized")
	}
	if p := r.writer.Append(env); p != nil {
		return p
	}
	return r.inner.Publish(ctx, env)
}

// Close flushes and closes fixture writer resources.
func (r *RecorderPublisher) Close() *problem.Problem {
	if r == nil || r.writer == nil {
		return nil
	}
	return r.writer.Close()
}
