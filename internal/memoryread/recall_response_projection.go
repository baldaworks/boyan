package memoryread

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *RecallService) responseFromProjection(
	ctx context.Context,
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
			return memory.RecallResponse{}, s.projectionAheadError(ctx, request.Scope, hits)
		}

		score, err := memory.RankRecallProjection(hit, record.CreatedAt, now)
		if err != nil {
			return memory.RecallResponse{}, err
		}

		results = append(
			results,
			recallReference(record, score),
		)
		seen[record.RevisionID] = struct{}{}
	}

	memory.SortRecallReferences(results)

	if len(results) > request.Limit {
		results = results[:request.Limit]
	}

	return recallResponse(request.Scope, currentSeq, results), nil
}

// projectionAheadError distinguishes a moved snapshot from invalid ahead metadata.
func (s *RecallService) projectionAheadError(
	ctx context.Context,
	scope memory.Scope,
	hits map[string]memory.RecallProjectionHit,
) error {
	currentSeq, err := s.canonical.CurrentScopeChangeSeq(ctx, scope)
	if err != nil {
		return err
	}

	for _, hit := range hits {
		if hit.ScopeChangeSeq > currentSeq {
			return memory.PermanentError(memory.CodeConflict, "recall projection is ahead of canonical state", nil)
		}
	}

	return memory.RetryableError(memory.CodeConflict, "recall canonical snapshot changed during projection query", nil)
}
