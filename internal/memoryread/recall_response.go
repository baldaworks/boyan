package memoryread

import memory "github.com/baldaworks/boyan/pkg/memory"

func emptyRecallResponse(scope memory.Scope, sequence uint64) memory.RecallResponse {
	return recallResponse(scope, sequence, []memory.RecallReference{})
}

func recallResponse(scope memory.Scope, sequence uint64, results []memory.RecallReference) memory.RecallResponse {
	return memory.RecallResponse{
		SchemaVersion:  memory.RecallSchemaVersionV1,
		Trust:          memory.ReferenceTrustUntrusted,
		Scope:          scope,
		ScopeChangeSeq: sequence,
		Results:        results,
	}
}
