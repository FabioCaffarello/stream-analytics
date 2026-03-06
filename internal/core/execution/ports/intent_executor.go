package ports

import (
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	strategydomain "github.com/market-raccoon/internal/core/strategy/domain"
)

// BoundaryInfo describes which execution adapter boundary produced the events.
// Stage 7 introduces an opt-in real adapter safe mode behind this same boundary.
type BoundaryInfo struct {
	Boundary string
	Adapter  string
	Mode     string
}

// IntentExecutor is the execution boundary consumed by runtime actors.
// Future real venue adapters must satisfy this interface behind the same port.
type IntentExecutor interface {
	ExecuteAt(intent strategydomain.StrategyIntentV1, observedAtMs int64) []executiondomain.ExecutionEventV1
	BoundaryInfo() BoundaryInfo
}
