package memory

type traceGraphIndex struct {
	revisions map[RevisionRef]RevisionMeta
	sources   map[SourceRef]SourceRecord
}

func indexTraceGraph(request TraceRequest, graph TraceGraph) (traceGraphIndex, error) {
	index := traceGraphIndex{
		revisions: make(map[RevisionRef]RevisionMeta, len(graph.Revisions)),
		sources:   make(map[SourceRef]SourceRecord, len(graph.Sources)),
	}
	for _, hit := range graph.Revisions {
		meta, ref, err := traceGraphRevisionEntry(request.Scope, hit)
		if err != nil {
			return traceGraphIndex{}, err
		}
		if _, exists := index.revisions[ref]; exists {
			return traceGraphIndex{}, invalidDerived("trace contains a duplicate revision")
		}
		index.revisions[ref] = meta
	}
	for _, source := range graph.Sources {
		if err := source.Validate(); err != nil {
			return traceGraphIndex{}, err
		}
		if source.Ref.Scope != request.Scope {
			return traceGraphIndex{}, PermanentError(CodeScopeViolation, "trace source scope does not match the request", nil)
		}
		if _, exists := index.sources[source.Ref]; exists {
			return traceGraphIndex{}, invalidDerived("trace contains a duplicate raw source")
		}
		index.sources[source.Ref] = source
	}
	return index, nil
}

func traceGraphRevisionEntry(scope Scope, hit SearchHit) (RevisionMeta, RevisionRef, error) {
	if hit.Score != nil {
		return RevisionMeta{}, RevisionRef{}, invalidDerived("trace revision cannot contain a search score")
	}
	meta, err := hit.Validate()
	if err != nil {
		return RevisionMeta{}, RevisionRef{}, err
	}
	if meta.Scope != scope {
		return RevisionMeta{}, RevisionRef{}, PermanentError(CodeScopeViolation, "trace revision scope does not match the request", nil)
	}
	if meta.State == RevisionStateInvalidated {
		return RevisionMeta{}, RevisionRef{}, PermanentError(CodeForgotten, "trace contains an invalidated revision", nil)
	}
	ref := RevisionRef{ItemID: meta.ItemID, RevisionID: meta.RevisionID}
	return meta, ref, nil
}
