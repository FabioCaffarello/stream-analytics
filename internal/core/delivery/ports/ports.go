package ports

import (
	"context"
	"encoding/json"

	"github.com/FabioCaffarello/stream-analytics/internal/core/delivery/domain"
	"github.com/FabioCaffarello/stream-analytics/internal/shared/problem"
)

// RangeItem is transport-agnostic historical output for getrange.
// Payload is json.RawMessage so it is inlined (not base64-encoded) when the
// outer frame is marshalled to JSON for WebSocket delivery.
type RangeItem struct {
	Seq      int64
	TsIngest int64
	Payload  json.RawMessage
}

// RangeStore resolves historical data for one Subject.
type RangeStore interface {
	GetRange(ctx context.Context, subject domain.Subject, fromMs, toMs int64, limit int) ([]RangeItem, *problem.Problem)
}
