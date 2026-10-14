package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
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

	if err := r.store.db.View(func(txn *badger.Txn) error { return requireSourcePrivacyReady(txn) }); err != nil {
		return memory.TraceResponse{}, canonicalStoreError("check canonical trace readiness", err)
	}

	graph, err := r.loadTraceGraph(ctx, normalized)
	if err != nil {
		return memory.TraceResponse{}, err
	}

	return buildTraceResponse(normalized, graph)
}
