package memory

const (
	traceVisiting = 1
	traceVisited  = 2
)

func validateTraceGraphClosure(root RevisionRef, index traceGraphIndex) error {
	colors := make(map[RevisionRef]uint8, len(index.revisions))

	visitedSources := make(map[SourceRef]struct{}, len(index.sources))
	if err := visitTraceGraph(root, index, colors, visitedSources); err != nil {
		return err
	}

	visitedRevisions := 0

	for _, color := range colors {
		if color == traceVisited {
			visitedRevisions++
		}
	}

	if visitedRevisions != len(index.revisions) || len(visitedSources) != len(index.sources) {
		return invalidDerived("trace graph contains nodes outside the requested provenance closure")
	}

	return nil
}

func visitTraceGraph(
	ref RevisionRef,
	index traceGraphIndex,
	colors map[RevisionRef]uint8,
	visitedSources map[SourceRef]struct{},
) error {
	switch colors[ref] {
	case traceVisiting:
		return invalidDerived("trace provenance contains a cycle")
	case traceVisited:
		return nil
	}

	meta, ok := index.revisions[ref]
	if !ok {
		return PermanentError(CodeNotFound, "trace provenance revision is missing", nil)
	}

	colors[ref] = traceVisiting

	for _, sourceRef := range meta.Provenance.RawSources {
		source, ok := index.sources[sourceRef]
		if !ok {
			return PermanentError(CodeNotFound, "trace provenance raw source is missing", nil)
		}

		if source.State == SourceStateForgotten {
			return PermanentError(CodeForgotten, "trace provenance source was forgotten", nil)
		}

		visitedSources[sourceRef] = struct{}{}
	}

	for _, parent := range meta.Provenance.ParentRevisions {
		if err := visitTraceGraph(parent, index, colors, visitedSources); err != nil {
			return err
		}
	}

	colors[ref] = traceVisited

	return nil
}
