package badger

import (
	"sort"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func buildTraceResponse(request memory.TraceRequest, graph traceGraph) (memory.TraceResponse, error) {
	itemByRevision := traceItemByRevision(graph.nodes)
	records, err := traceRecords(request.Scope, graph, itemByRevision)
	if err != nil {
		return memory.TraceResponse{}, err
	}
	response := memory.TraceResponse{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Trust:         memory.ReferenceTrustUntrusted,
		Scope:         request.Scope,
		Root:          request.Root,
		Revisions:     records,
		Sources:       sortedTraceSources(graph.sourcesByRef),
	}
	if err := response.Validate(request.MaxNodes); err != nil {
		return memory.TraceResponse{}, err
	}
	return response, nil
}

func sortedTraceSources(sourcesByRef map[memory.SourceRef]memory.SourceRecord) []memory.SourceRecord {
	sources := make([]memory.SourceRecord, 0, len(sourcesByRef))
	for _, source := range sourcesByRef {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Ref.ExportID < sources[j].Ref.ExportID
	})
	return sources
}
