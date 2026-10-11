package mcp

import (
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func buildSearchRequest(
	in searchServiceInput,
	scope memory.Scope,
	query string,
	limit int,
) (
	memory.RecallRequest,
	error,
) {
	return memory.NormalizeRecallRequest(memory.RecallRequest{
		SchemaVersion:     memory.RecallSchemaVersionV1,
		Scope:             scope,
		Query:             query,
		Limit:             limit,
		Kind:              cloneMemoryKind(in.MemoryKind),
		Category:          cloneCategory(in.Category),
		AsOf:              cloneTime(in.AsOf),
		SourceID:          strings.TrimSpace(in.SourceID),
		ConversationID:    strings.TrimSpace(in.ConversationID),
		MemoryKey:         memory.MemoryKey(strings.TrimSpace(in.MemoryKey)),
		MinScopeChangeSeq: in.MinScopeChangeSeq,
	})
}
