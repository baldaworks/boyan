package memoryread

import (
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *RecallService) responseFromProjection(
	request memory.RecallRequest,
	records []memory.RecallRecord,
	hits map[string]memory.RecallProjectionHit,
	currentSeq uint64,
	now time.Time,
) (memory.RecallResponse, error) {
	results := make([]memory.RecallReference, 0, request.Limit)

	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		hit, exists := hits[record.RevisionID]
		if !exists {
			continue
		}

		if _, exists := seen[record.RevisionID]; exists {
			continue
		}

		if err := record.Validate(request, now); err != nil {
			if skipStaleRecallError(err) {
				continue
			}

			return memory.RecallResponse{}, err
		}

		if hit.ScopeChangeSeq != 0 && hit.ScopeChangeSeq > currentSeq {
			return memory.RecallResponse{}, memory.PermanentError(
				memory.CodeConflict,
				"recall projection is ahead of canonical state",
				nil,
			)
		}

		results = append(
			results,
			recallReference(record, memory.RankRecall(hit.Score, record.CreatedAt, now)),
		)
		seen[record.RevisionID] = struct{}{}
	}

	memory.SortRecallReferences(results)

	if len(results) > request.Limit {
		results = results[:request.Limit]
	}

	return recallResponse(request.Scope, currentSeq, results), nil
}
