package memoryread

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *RecallService) searchProjectionOrTail(
	ctx context.Context,
	request memory.RecallRequest,
	currentSeq uint64,
	now time.Time,
) (memory.RecallResponse, error) {
	if s.projection == nil {
		return s.searchCanonicalTail(ctx, request, currentSeq, now)
	}

	hits, err := s.projection.SearchRecall(ctx, request)
	if err != nil {
		return memory.RecallResponse{}, err
	}

	if len(hits) > memory.MaxRecallCandidates {
		return memory.RecallResponse{}, memory.PermanentError(
			memory.CodeLimitExceeded,
			"recall projection candidate bound exceeded",
			nil,
		)
	}

	if len(hits) == 0 {
		return s.searchCanonicalTail(ctx, request, currentSeq, now)
	}

	ids, byID, err := projectionRecallHits(request.Scope, hits)
	if err != nil {
		return memory.RecallResponse{}, err
	}

	if len(ids) == 0 {
		return emptyRecallResponse(request.Scope, currentSeq), nil
	}

	records, err := s.canonical.LoadRecallRecords(ctx, request.Scope, ids)
	if err != nil {
		return memory.RecallResponse{}, err
	}

	return s.responseFromProjection(request, records, byID, currentSeq, now)
}
