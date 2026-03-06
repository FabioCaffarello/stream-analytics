package ports

import (
	executiondomain "github.com/market-raccoon/internal/core/execution/domain"
	"github.com/market-raccoon/internal/shared/problem"
)

// ControlPlane is the runtime governance boundary.
// Implementations must be safe for concurrent read access.
type ControlPlane interface {
	Snapshot() executiondomain.ControlSnapshot
	Apply(directive executiondomain.ControlDirective) *problem.Problem
}
