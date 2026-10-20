package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type traceNode struct {
	view canonicalRecallView
	text []byte
}
type traceGraph struct {
	nodes        []traceNode
	sourcesByRef map[memory.SourceRef]memory.SourceRecord
	sourceIDs    map[string]memory.CanonicalSourceRecord
	scopeVersion uint64
}

func (r *CanonicalReader) loadTraceGraph(ctx context.Context, request memory.TraceRequest) (traceGraph, error) {
	graph, err := r.preflightTraceGraph(ctx, request)
	if err != nil {
		return traceGraph{}, err
	}

	for i := range graph.nodes {
		node := &graph.nodes[i]

		err := r.traceReadTxn(ctx, func(txn *badger.Txn) error {
			return loadCanonicalViewSources(txn, request.Scope, &node.view)
		})
		if err != nil {
			return traceGraph{}, err
		}

		text, err := r.loadTracePayload(ctx, node.view.revision.Payload, maxCanonicalValueBytes)
		if err != nil {
			return traceGraph{}, err
		}

		node.text = text
		if compatibility, ok := canonicalCompatibilityPayload(text, request.Scope); ok {
			node.view.compat = &compatibility
		}

		if err := r.loadTraceSources(ctx, request.Scope, node.view, graph.sourcesByRef); err != nil {
			return traceGraph{}, err
		}

		if len(node.view.sourceRefs) == 0 {
			return traceGraph{}, traceDenied()
		}
	}

	return graph, nil
}
