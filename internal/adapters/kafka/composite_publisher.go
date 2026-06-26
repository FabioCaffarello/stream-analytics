package kafka

import (
	"context"
	"log/slog"

	"github.com/FabioCaffarello/stream-analytics/internal/shared/envelope"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// CompositePublisher fans out to two publishers in sequence.
// If primary fails, the error is returned and secondary is not called.
// If secondary fails, the error is logged and swallowed (best-effort analytics).
type CompositePublisher struct {
	primary   publisherIface
	secondary publisherIface
}

type publisherIface interface {
	Publish(ctx context.Context, env envelope.Envelope) *problem.Problem
}

// NewCompositePublisher wraps two publishers so both receive every event.
func NewCompositePublisher(primary, secondary publisherIface) *CompositePublisher {
	return &CompositePublisher{primary: primary, secondary: secondary}
}

// Publish calls primary first. If it succeeds, calls secondary as best-effort.
func (c *CompositePublisher) Publish(ctx context.Context, env envelope.Envelope) *problem.Problem {
	if p := c.primary.Publish(ctx, env); p != nil {
		return p
	}
	// Secondary (analytics) is best-effort — log errors but don't fail the primary path.
	if p := c.secondary.Publish(ctx, env); p != nil {
		slog.Warn("analytics kafka publish failed", "err", p, "type", env.Type)
	}
	return nil
}
