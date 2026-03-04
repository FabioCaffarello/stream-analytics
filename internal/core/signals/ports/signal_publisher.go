package ports

import (
	"context"

	signalsdomain "github.com/market-raccoon/internal/core/signals/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// SignalPublisher is the transport-neutral output port for composite signals.
type SignalPublisher interface {
	PublishSignal(ctx context.Context, signal signalsdomain.CompositeSignalV1) *problem.Problem
}
