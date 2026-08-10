package badger

import (
	"context"
	"sort"

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
	queue := []string{normalized.Root.RevisionID}
	seen := make(map[string]struct{}, normalized.MaxNodes)
	type traceNode struct {
		view canonicalRecallView
		text []byte
	}
	nodes := make([]traceNode, 0, normalized.MaxNodes)
	sourcesByRef := make(map[memory.SourceRef]memory.SourceRecord)
	for len(queue) > 0 {
		if len(seen) >= normalized.MaxNodes {
			return memory.TraceResponse{}, memory.PermanentError(memory.CodeLimitExceeded, "canonical trace exceeds node bound", nil)
		}
		revisionID := queue[0]
		queue = queue[1:]
		if _, exists := seen[revisionID]; exists {
			continue
		}
		view, found, loadErr := r.loadCanonicalView(ctx, normalized.Scope, revisionID, false)
		if loadErr != nil {
			return memory.TraceResponse{}, loadErr
		}
		if !found {
			return memory.TraceResponse{}, memory.PermanentError(memory.CodeForgotten, "canonical trace revision is unavailable", nil)
		}
		if revisionID == normalized.Root.RevisionID && view.item.ItemID != normalized.Root.ItemID {
			return memory.TraceResponse{}, memory.PermanentError(memory.CodeScopeViolation, "canonical trace root item does not match", nil)
		}
		seen[revisionID] = struct{}{}
		queue = append(queue, view.revision.Parents...)
		text, payloadErr := r.store.LoadPayload(ctx, view.revision.Payload)
		if payloadErr != nil {
			return memory.TraceResponse{}, payloadErr
		}
		if compatibility, ok := canonicalCompatibilityPayload(text, normalized.Scope); ok {
			view.compat = &compatibility
		}
		for index, source := range view.sourceRefs {
			if _, exists := sourcesByRef[source]; exists {
				continue
			}
			sourceID := source.ExportID
			if index < len(view.sourceIDs) {
				sourceID = view.sourceIDs[index]
			}
			sourceRecord, sourceErr := r.loadSourceRecord(ctx, normalized.Scope, source, sourceID)
			if sourceErr != nil {
				return memory.TraceResponse{}, sourceErr
			}
			if sourceRecord.Ref != source {
				return memory.TraceResponse{}, memory.PermanentError(memory.CodeScopeViolation, "canonical trace source identity does not match", nil)
			}
			sourcesByRef[source] = sourceRecord
		}
		if len(view.sourceRefs) == 0 {
			return memory.TraceResponse{}, memory.PermanentError(memory.CodeForgotten, "canonical trace provenance is unavailable", nil)
		}
		if len(nodes)+len(sourcesByRef) > normalized.MaxNodes {
			return memory.TraceResponse{}, memory.PermanentError(memory.CodeLimitExceeded, "canonical trace exceeds node bound", nil)
		}
		nodes = append(nodes, traceNode{view: view, text: text})
	}
	itemByRevision := make(map[string]string, len(nodes))
	for _, node := range nodes {
		itemByRevision[node.view.revision.RevisionID] = node.view.item.ItemID
	}
	records := make([]memory.SearchHit, 0, len(nodes))
	for _, node := range nodes {
		view := node.view
		provenance := memory.Provenance{ParentRevisions: make([]memory.RevisionRef, 0, len(view.revision.Parents))}
		for _, parentID := range view.revision.Parents {
			parentItemID, ok := itemByRevision[parentID]
			if !ok {
				return memory.TraceResponse{}, memory.PermanentError(memory.CodeNotFound, "canonical trace parent revision is missing", nil)
			}
			provenance.ParentRevisions = append(provenance.ParentRevisions, memory.RevisionRef{ItemID: parentItemID, RevisionID: parentID})
		}
		for _, source := range view.sourceRefs {
			if _, ok := sourcesByRef[source]; ok {
				provenance.RawSources = append(provenance.RawSources, source)
			}
		}
		if err := provenance.Validate(normalized.Scope); err != nil {
			return memory.TraceResponse{}, err
		}
		hit, err := canonicalTraceHit(normalized.Scope, view, node.text, provenance, categoryForCanonicalView(view))
		if err != nil {
			return memory.TraceResponse{}, err
		}
		records = append(records, hit)
	}
	root := normalized.Root
	sources := make([]memory.SourceRecord, 0, len(sourcesByRef))
	for _, source := range sourcesByRef {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Ref.ExportID < sources[j].Ref.ExportID })
	response := memory.TraceResponse{SchemaVersion: memory.DerivedSchemaVersionV1, Trust: memory.ReferenceTrustUntrusted, Scope: normalized.Scope, Root: root, Revisions: records, Sources: sources}
	if err := response.Validate(normalized.MaxNodes); err != nil {
		return memory.TraceResponse{}, err
	}
	return response, nil
}
