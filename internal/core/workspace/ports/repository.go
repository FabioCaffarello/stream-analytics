// Package ports defines the workspace bounded context's driven ports.
package ports

import (
	"github.com/FabioCaffarello/stream-analytics/internal/core/workspace/domain"
)

// WorkspaceRepository abstracts load/save of the workspace aggregate.
// A nil return from Load (with nil error) means no state has been persisted yet.
type WorkspaceRepository interface {
	Load() (*domain.Workspace, error)
	Save(ws *domain.Workspace) error
	Delete() error
}
