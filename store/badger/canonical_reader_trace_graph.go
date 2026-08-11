package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type traceNode struct {
	view canonicalRecallView
	text []byte
}

type traceGraph struct {
	nodes        []traceNode
	sourcesByRef map[memory.SourceRef]memory.SourceRecord
}

func (r *CanonicalReader) loadTraceGraph(ctx context.Context, request memory.TraceRequest) (traceGraph, error) {
	queue := []string{request.Root.RevisionID}
	seen := make(map[string]struct{}, request.MaxNodes)
	graph := traceGraph{
		nodes:        make([]traceNode, 0, request.MaxNodes),
		sourcesByRef: make(map[memory.SourceRef]memory.SourceRecord),
	}
	for len(queue) > 0 {
		if len(seen) >= request.MaxNodes {
			return traceGraph{}, memory.PermanentError(memory.CodeLimitExceeded, "canonical trace exceeds node bound", nil)
		}
		revisionID := queue[0]
		queue = queue[1:]
		if _, exists := seen[revisionID]; exists {
			continue
		}
		node, parents, err := r.loadTraceNode(ctx, request, revisionID, graph.sourcesByRef)
		if err != nil {
			return traceGraph{}, err
		}
		seen[revisionID] = struct{}{}
		queue = append(queue, parents...)
		if len(graph.nodes)+len(graph.sourcesByRef) > request.MaxNodes {
			return traceGraph{}, memory.PermanentError(memory.CodeLimitExceeded, "canonical trace exceeds node bound", nil)
		}
		graph.nodes = append(graph.nodes, node)
	}
	return graph, nil
}
