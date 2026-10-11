package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *CanonicalReader) loadTraceNode(
	ctx context.Context,
	request memory.TraceRequest,
	revisionID string,
	sourcesByRef map[memory.SourceRef]memory.SourceRecord,
) (traceNode, []string, error) {
	view, found, err := r.loadCanonicalView(ctx, request.Scope, revisionID, false)
	if err != nil {
		return traceNode{}, nil, err
	}

	if !found {
		return traceNode{}, nil, memory.PermanentError(memory.CodeForgotten, "canonical trace revision is unavailable", nil)
	}

	if revisionID == request.Root.RevisionID && view.item.ItemID != request.Root.ItemID {
		return traceNode{}, nil, memory.PermanentError(
			memory.CodeScopeViolation,
			"canonical trace root item does not match",
			nil,
		)
	}

	text, err := r.store.LoadPayload(ctx, view.revision.Payload)
	if err != nil {
		return traceNode{}, nil, err
	}

	if compatibility, ok := canonicalCompatibilityPayload(text, request.Scope); ok {
		view.compat = &compatibility
	}

	if err := r.loadTraceSources(ctx, request.Scope, view, sourcesByRef); err != nil {
		return traceNode{}, nil, err
	}

	if len(view.sourceRefs) == 0 {
		return traceNode{}, nil, memory.PermanentError(memory.CodeForgotten, "canonical trace provenance is unavailable", nil)
	}

	return traceNode{view: view, text: text}, view.revision.Parents, nil
}
