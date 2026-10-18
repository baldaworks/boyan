package canonicalsql

import (
	"sort"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func derivedSearchResponse(
	normalized memory.DerivedSearchRequest,
	results []memory.DerivedReference,
) (memory.DerivedSearchResponse, error) {
	sort.SliceStable(results, func(i, j int) bool {
		if !results[i].CreatedAt.Equal(results[j].CreatedAt) {
			return results[i].CreatedAt.After(results[j].CreatedAt)
		}

		return results[i].RevisionID < results[j].RevisionID
	})

	if len(results) > normalized.Limit {
		results = results[:normalized.Limit]
	}

	response := memory.DerivedSearchResponse{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Trust:         memory.ReferenceTrustUntrusted,
		Scope:         normalized.Scope,
		Results:       results,
	}
	if err := response.Validate(normalized.Limit); err != nil {
		return memory.DerivedSearchResponse{}, err
	}

	return response, nil
}
