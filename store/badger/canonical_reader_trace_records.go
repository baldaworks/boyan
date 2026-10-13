package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func traceItemByRevision(nodes []traceNode) map[string]memory.RevisionRef {
	itemByRevision := make(map[string]memory.RevisionRef, len(nodes))
	for _, node := range nodes {
		ref := memory.RevisionRef{ItemID: node.view.item.ItemID, RevisionID: node.view.revision.RevisionID}
		if node.view.compat != nil {
			ref = memory.RevisionRef{ItemID: node.view.compat.LegacyItemID, RevisionID: node.view.compat.LegacyRevisionID}
		}

		itemByRevision[node.view.revision.RevisionID] = ref
	}

	return itemByRevision
}

func traceRecords(scope memory.Scope,
	graph traceGraph,
	itemByRevision map[string]memory.RevisionRef) ([]memory.SearchHit,
	error) {
	records := make([]memory.SearchHit, 0, len(graph.nodes))
	for _, node := range graph.nodes {
		provenance, err := traceProvenance(scope, node, itemByRevision, graph.sourcesByRef)
		if err != nil {
			return nil, err
		}

		hit, err := canonicalTraceHit(scope, node.view, node.text, provenance, categoryForCanonicalView(node.view))
		if err != nil {
			return nil, err
		}

		records = append(records, hit)
	}

	return records, nil
}

func traceProvenance(
	scope memory.Scope,
	node traceNode,
	itemByRevision map[string]memory.RevisionRef,
	sourcesByRef map[memory.SourceRef]memory.SourceRecord,
) (memory.Provenance, error) {
	provenance := memory.Provenance{
		ParentRevisions: make([]memory.RevisionRef, 0, len(node.view.revision.Parents)),
	}
	for _, parentID := range node.view.revision.Parents {
		parentRef, ok := itemByRevision[parentID]
		if !ok {
			return memory.Provenance{}, memory.PermanentError(
				memory.CodeNotFound,
				"canonical trace parent revision is missing",
				nil,
			)
		}

		provenance.ParentRevisions = append(provenance.ParentRevisions, parentRef)
	}

	for _, source := range node.view.sourceRefs {
		if _, ok := sourcesByRef[source]; ok {
			provenance.RawSources = append(provenance.RawSources, source)
		}
	}

	if err := provenance.Validate(scope); err != nil {
		return memory.Provenance{}, err
	}

	return provenance, nil
}
