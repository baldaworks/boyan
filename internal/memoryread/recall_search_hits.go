package memoryread

import memory "github.com/baldaworks/boyan/pkg/memory"

func projectionRecallHits(
	scope memory.Scope,
	hits []memory.RecallProjectionHit,
) (
	[]string,
	map[string]memory.RecallProjectionHit,
	error,
) {
	ids := make([]string, 0, len(hits))

	byID := make(map[string]memory.RecallProjectionHit, len(hits))
	for _, hit := range hits {
		if hit.Scope != scope {
			return nil, nil, memory.PermanentError(memory.CodeScopeViolation, "recall projection returned a foreign scope", nil)
		}

		if hit.RevisionID == "" {
			return nil, nil, memory.PermanentError(
				memory.CodeInvalidDerived,
				"recall projection returned an empty revision",
				nil,
			)
		}

		if _, exists := byID[hit.RevisionID]; exists {
			continue
		}

		byID[hit.RevisionID] = hit
		ids = append(ids, hit.RevisionID)
	}

	return ids, byID, nil
}
