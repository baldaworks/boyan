package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (o *Owner) preflightTraceGraph(ctx context.Context, request memory.TraceRequest) (traceGraph, error) {
	graph := traceGraph{sourcesByRef: make(map[memory.SourceRef]memory.SourceRecord),
		rawByID: make(map[string]memory.SourceRecord), captured: readerSnapshot{
			revisions: make(map[string]memory.MemoryRevision), sources: make(map[string]memory.CanonicalSourceRecord)}}
	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		state, err := traceScopeState(ctx, tx, request.Scope)
		if err != nil {
			return err
		}

		graph.captured.state = state

		return collectTraceClosure(ctx, tx, request, &graph)
	})

	return graph, err
}

func traceScopeState(ctx context.Context, tx *Tx, scope memory.Scope) (memory.ScopeState, error) {
	state, err := readerScopeState(ctx, tx, scope)
	if err != nil {
		return state, err
	}

	incomplete, err := privacyScopeIncomplete(ctx, tx, scope)
	if err != nil {
		return state, err
	}

	if incomplete {
		return state, readerDenied()
	}

	return state, nil
}

func loadTraceViewCore(ctx context.Context, tx *Tx, scope memory.Scope, id string) (traceView, error) {
	view := traceView{canonicalRecallView: canonicalRecallView{state: memory.RevisionStateActive}}

	revision, err := loadProofRevision(ctx, tx, scope, id)
	if err != nil {
		return view, err
	}

	if revision.RevisionID != id || len(revision.Parents) > memory.MaxSourcesPerRevision ||
		len(revision.Evidence) > memory.MaxSourcesPerRevision {
		return view, invalidSourcePrivacy()
	}

	if revision.Sensitivity != memory.SensitivityStandard {
		return view, readerDenied()
	}

	view.revision = revision

	view.item, err = loadProofItem(ctx, tx, scope, revision.ItemID)
	if err != nil {
		return view, err
	}

	return completeTraceView(ctx, tx, scope, view)
}

func completeTraceView(ctx context.Context, tx *Tx, scope memory.Scope, view traceView) (traceView, error) {
	id := view.revision.RevisionID

	key, err := privacyKey(scope, Head, view.item.ItemID)
	if err != nil {
		return view, err
	}

	var head memory.ItemHead
	if err := getPrivacyRecord(ctx, tx, key, Head, &head); err != nil {
		return view, err
	}

	if head.Validate() != nil || head.ItemID != view.item.ItemID {
		return view, corruptRow()
	}

	if head.RevisionID != id {
		view.state = memory.RevisionStateSuperseded
	}

	if err := checkTraceDenial(ctx, tx, scope, DeniedRevision, id); err != nil {
		return view, err
	}

	return view, nil
}

func collectTraceClosure(ctx context.Context, tx *Tx, request memory.TraceRequest, graph *traceGraph) error {
	queue := []string{request.Root.RevisionID}
	seen := map[string]bool{request.Root.RevisionID: true}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		id := queue[0]
		queue = queue[1:]

		view, err := loadTraceViewCore(ctx, tx, request.Scope, id)
		if err != nil {
			return err
		}

		if id == request.Root.RevisionID && view.item.ItemID != request.Root.ItemID {
			return memory.PermanentError(memory.CodeScopeViolation, "canonical trace root item does not match", nil)
		}

		if err := preflightTraceSources(ctx, tx, request, view, len(seen), graph); err != nil {
			return err
		}

		graph.nodes = append(graph.nodes, traceNode{view: view})
		graph.captured.revisions[id] = view.revision

		queue, err = enqueueTraceParents(queue, seen, view.revision.Parents, len(graph.captured.sources), request.MaxNodes)
		if err != nil {
			return err
		}
	}

	return nil
}

func preflightTraceSource(ctx context.Context, tx *Tx, scope memory.Scope,
	id string) (memory.CanonicalSourceRecord, error) {
	if err := checkTraceDenial(ctx, tx, scope, DeniedSource, id); err != nil {
		return memory.CanonicalSourceRecord{}, err
	}

	source, found, err := loadPrivacySource(ctx, tx, scope, id)
	if err != nil {
		return source, err
	}

	if !found {
		return source, readerDenied()
	}

	record, found, err := loadSourcePrivacy(ctx, tx, source)
	if err != nil {
		return source, err
	}

	if !found || record.Preparing || record.State != sourcePrivacyStandard ||
		source.Sensitivity != memory.SensitivityStandard {
		return source, readerDenied()
	}

	return source, nil
}

func enqueueTraceParents(queue []string, seen map[string]bool, parents []string, sources, limit int) ([]string, error) {
	if len(seen)+sources > limit {
		return nil, traceNodeLimit()
	}

	for _, parent := range parents {
		if seen[parent] {
			continue
		}

		if len(seen)+sources >= limit {
			return nil, traceNodeLimit()
		}

		seen[parent] = true
		queue = append(queue, parent)
	}

	return queue, nil
}

func traceNodeLimit() error {
	return memory.PermanentError(memory.CodeLimitExceeded, "canonical trace exceeds node bound", nil)
}

func checkTraceDenial(ctx context.Context, tx *Tx, scope memory.Scope, family Family, id string) error {
	denied, err := privacyDenied(ctx, tx, scope, family, id)
	if err != nil {
		return err
	}

	if denied {
		return readerDenied()
	}

	return nil
}

func preflightTraceSources(ctx context.Context, tx *Tx, request memory.TraceRequest,
	view traceView, revisions int, graph *traceGraph) error {
	if len(view.revision.Evidence) == 0 {
		return readerDenied()
	}

	for _, ref := range view.revision.Evidence {
		if _, exists := graph.captured.sources[ref.SourceID]; exists {
			continue
		}

		if revisions+len(graph.captured.sources) >= request.MaxNodes {
			return traceNodeLimit()
		}

		source, err := preflightTraceSource(ctx, tx, request.Scope, ref.SourceID)
		if err != nil {
			return err
		}

		graph.captured.sources[ref.SourceID] = source
	}

	return nil
}
