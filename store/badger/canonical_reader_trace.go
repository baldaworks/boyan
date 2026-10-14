package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Trace returns a bounded parent closure for one exact canonical scope.
func (r *CanonicalReader) Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return memory.TraceResponse{}, err
	}

	normalized, err := memory.NormalizeTraceRequest(request, memory.MaxTraceNodes)
	if err != nil {
		return memory.TraceResponse{}, err
	}

	if !r.hasOpenStore() {
		return memory.TraceResponse{}, invalidSourcePrivacy()
	}

	graph, err := r.loadTraceGraph(ctx, normalized)
	if err != nil {
		return memory.TraceResponse{}, publicTraceError(err)
	}

	response, err := buildTraceResponse(normalized, graph)
	if err != nil {
		return memory.TraceResponse{}, publicTraceError(err)
	}

	if err := r.finalTracePrivacyBarrier(ctx, normalized.Scope, graph); err != nil {
		return memory.TraceResponse{}, publicTraceError(err)
	}

	if r.afterTracePrivacyBarrier != nil {
		r.afterTracePrivacyBarrier()
	}

	return response, nil
}
