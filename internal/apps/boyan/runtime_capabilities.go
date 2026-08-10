package app

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

// Search implements RecallReader.
func (r *Runtime) Search(ctx context.Context, request memory.RecallRequest) (memory.RecallResponse, error) {
	if r == nil || r.recall == nil {
		return memory.RecallResponse{}, memory.PermanentError(memory.CodeDisabled, "boyan recall is unavailable", nil)
	}
	return r.recall.Search(ctx, request)
}

// Trace implements TraceReader.
func (r *Runtime) Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error) {
	if r == nil || r.trace == nil {
		return memory.TraceResponse{}, memory.PermanentError(memory.CodeDisabled, "boyan trace is unavailable", nil)
	}
	return r.trace.Trace(ctx, request)
}

// ForgetSource implements Forgetter.
func (r *Runtime) ForgetSource(ctx context.Context, command memory.ForgetSourceCommand) (memory.ForgetOutcome, error) {
	if r == nil || r.forget == nil {
		return memory.ForgetOutcome{}, memory.PermanentError(memory.CodeDisabled, "boyan forget is unavailable", nil)
	}
	return r.forget.ForgetSource(ctx, command)
}

// ForgetScope implements Forgetter.
func (r *Runtime) ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error) {
	if r == nil || r.forget == nil {
		return memory.ForgetOutcome{}, memory.PermanentError(memory.CodeDisabled, "boyan forget is unavailable", nil)
	}
	return r.forget.ForgetScope(ctx, command)
}
