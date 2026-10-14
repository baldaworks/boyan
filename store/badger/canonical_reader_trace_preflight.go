package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadTraceViewCore(txn *badger.Txn, scope memory.Scope, id string, view *canonicalRecallView) error {
	key, err := canonicalKey(scope, badgerRecordRevision, id)
	if err != nil {
		return err
	}

	if err := checkPrivacyPreparationValue(txn, key); err != nil {
		return err
	}

	if err := getPrivacyPreparationRecord(txn, key, badgerRecordRevision, &view.revision); err != nil {
		return err
	}

	if !traceRevisionIdentityValid(view.revision, id) {
		return invalidSourcePrivacy()
	}

	if view.revision.Sensitivity != memory.SensitivityStandard {
		return traceDenied()
	}

	if err := traceViewMetadataBounds(txn, scope, view.revision.ItemID); err != nil {
		return err
	}

	if err := loadCanonicalViewItem(txn, scope, view); err != nil {
		return err
	}

	if err := loadCanonicalViewHead(txn, scope, false, view); err != nil {
		return err
	}

	return checkDeniedRevision(txn, scope, id)
}

func (r *CanonicalReader) preflightTraceGraph(ctx context.Context, request memory.TraceRequest) (traceGraph, error) {
	graph := traceGraph{
		sourcesByRef: make(map[memory.SourceRef]memory.SourceRecord),
		sourceIDs:    make(map[string]memory.CanonicalSourceRecord),
	}
	err := r.traceReadTxn(ctx, func(txn *badger.Txn) error {
		version, err := r.traceScopeVersion(txn, request.Scope)
		if err != nil {
			return err
		}

		graph.scopeVersion = version

		return collectTraceClosure(ctx, txn, request, &graph)
	})

	return graph, err
}

func collectTraceClosure(ctx context.Context, txn *badger.Txn, request memory.TraceRequest, graph *traceGraph) error {
	queue := []string{request.Root.RevisionID}
	seen := map[string]bool{request.Root.RevisionID: true}

	for len(queue) > 0 {
		if err := canonicalReaderContext(ctx); err != nil {
			return err
		}

		id := queue[0]
		queue = queue[1:]

		view := canonicalRecallView{state: memory.RevisionStateActive}
		if err := loadTraceViewCore(txn, request.Scope, id, &view); err != nil {
			return err
		}

		if id == request.Root.RevisionID && view.item.ItemID != request.Root.ItemID {
			return memory.PermanentError(memory.CodeScopeViolation, "canonical trace root item does not match", nil)
		}

		if err := preflightTraceSources(txn, request, &view, graph); err != nil {
			return err
		}

		graph.nodes = append(graph.nodes, traceNode{view: view})

		var err error

		queue, err = enqueueTraceParents(queue, seen, view.revision.Parents, len(graph.sourceIDs), request.MaxNodes)
		if err != nil {
			return err
		}
	}

	return nil
}

func preflightTraceSources(
	txn *badger.Txn, request memory.TraceRequest, view *canonicalRecallView, graph *traceGraph,
) error {
	if len(view.revision.Evidence) == 0 {
		return traceDenied()
	}

	for _, evidence := range view.revision.Evidence {
		if _, exists := graph.sourceIDs[evidence.SourceID]; exists {
			continue
		}

		if len(graph.nodes)+len(graph.sourceIDs)+2 > request.MaxNodes {
			return traceNodeLimit()
		}

		source, err := authorizeTraceSource(txn, request.Scope, evidence.SourceID)
		if err != nil {
			return err
		}

		graph.sourceIDs[evidence.SourceID] = source
	}

	return nil
}

func traceNodeLimit() error {
	return memory.PermanentError(memory.CodeLimitExceeded, "canonical trace exceeds node bound", nil)
}

func traceViewMetadataBounds(txn *badger.Txn, scope memory.Scope, itemID string) error {
	for _, kind := range []string{badgerRecordItem, badgerRecordHead} {
		key, err := canonicalKey(scope, kind, itemID)
		if err != nil {
			return err
		}

		if err := checkPrivacyPreparationValue(txn, key); err != nil {
			return err
		}
	}

	return nil
}

func traceRevisionIdentityValid(revision memory.MemoryRevision, id string) bool {
	return revision.Validate() == nil && len(revision.Parents) <= memory.MaxSourcesPerRevision &&
		len(revision.Evidence) <= memory.MaxSourcesPerRevision && revision.RevisionID == id
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
