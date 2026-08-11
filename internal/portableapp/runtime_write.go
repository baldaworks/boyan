package portableapp

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// IngestTurn implements TurnIngestor.
func (r *Runtime) IngestTurn(ctx context.Context, turn memory.Turn) error {
	if r == nil || r.turn == nil {
		return memory.PermanentError(memory.CodeDisabled, "boyan turn ingestion is unavailable", nil)
	}
	return r.turn.IngestTurn(ctx, turn)
}

// ApplyBoundary implements BoundaryIngestor.
func (r *Runtime) ApplyBoundary(ctx context.Context, boundary memory.Boundary) error {
	if r == nil || r.boundary == nil {
		return memory.PermanentError(memory.CodeDisabled, "boyan boundary ingestion is unavailable", nil)
	}
	return r.boundary.ApplyBoundary(ctx, boundary)
}
