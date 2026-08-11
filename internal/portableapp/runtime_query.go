package portableapp

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

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
