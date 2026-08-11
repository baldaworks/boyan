package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func derivedRecallRequest(normalized memory.DerivedSearchRequest) memory.RecallRequest {
	return memory.RecallRequest{
		Scope:             normalized.Scope,
		Query:             normalized.Query,
		Limit:             normalized.Limit,
		AsOf:              normalized.AsOf,
		SourceID:          normalized.SourceID,
		ConversationID:    normalized.ConversationID,
		MinScopeChangeSeq: normalized.MinScopeChangeSeq,
	}
}
