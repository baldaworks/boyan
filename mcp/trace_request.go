package mcp

import (
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func normalizeTraceRoot(in traceServiceInput) (memory.RevisionRef, error) {
	root := memory.RevisionRef{
		ItemID:     strings.TrimSpace(in.ItemID),
		RevisionID: strings.TrimSpace(in.RevisionID),
	}
	if err := root.Validate(); err != nil {
		return memory.RevisionRef{}, err
	}
	return root, nil
}

func buildTraceRequest(in traceServiceInput, scope memory.Scope, root memory.RevisionRef) (memory.TraceRequest, error) {
	return memory.NormalizeTraceRequest(memory.TraceRequest{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Scope:         scope,
		Root:          root,
		MaxNodes:      in.MaxNodes,
	}, memory.MaxTraceNodes)
}
