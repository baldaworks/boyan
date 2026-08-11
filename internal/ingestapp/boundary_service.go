package ingestapp

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// BoundaryService coordinates a host-provided boundary semantic processor
// with the rebuildable projection.  The semantic processor itself is a narrow
// port so a future service adapter can replace it without changing Runtime.
type BoundaryService struct {
	processor  BoundaryExecutor
	projection ProjectionSyncer
}

// NewBoundaryService constructs the boundary application service.
func NewBoundaryService(processor BoundaryExecutor, projection ProjectionSyncer) (*BoundaryService, error) {
	if processor == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "boundary processor is required", nil)
	}
	return &BoundaryService{processor: processor, projection: projection}, nil
}

// ApplyBoundary implements BoundaryIngestor.
func (s *BoundaryService) ApplyBoundary(ctx context.Context, boundary memory.Boundary) error {
	if s == nil || s.processor == nil {
		return memory.PermanentError(memory.CodeDisabled, "boundary processor is unavailable", nil)
	}
	if err := boundary.Validate(); err != nil {
		return err
	}
	if _, err := s.processor.ProcessBoundary(ctx, boundary); err != nil {
		return err
	}
	if s.projection != nil {
		return s.projection.Sync(ctx, boundary.Scope)
	}
	return nil
}

var _ BoundaryIngestor = (*BoundaryService)(nil)
