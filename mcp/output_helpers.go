package mcp

import memory "github.com/baldaworks/boyan/pkg/memory"

func copyRecallReferences(results []memory.RecallReference) []Reference {
	out := make([]Reference, 0, len(results))
	for _, result := range results {
		category := cloneCategory(result.Category)
		explain := result.Explain
		evidence := append([]memory.EvidenceRef(nil), result.Evidence...)
		out = append(out, Reference{
			ID:             result.RevisionID,
			ScopeKey:       result.Scope.Key,
			Text:           result.Text,
			CreatedAt:      result.CreatedAt,
			Score:          floatPointer(result.Score),
			MemoryKind:     result.Kind,
			ItemID:         result.ItemID,
			RevisionID:     result.RevisionID,
			Revision:       result.Revision,
			State:          result.State,
			Category:       category,
			MemoryKey:      result.MemoryKey,
			Evidence:       evidence,
			Explain:        &explain,
			ScopeChangeSeq: result.ScopeChangeSeq,
		})
	}

	return out
}

func floatPointer(value float64) *float64 {
	copyOfValue := value

	return &copyOfValue
}
