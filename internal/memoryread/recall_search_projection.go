package memoryread

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop // Every successful projection or canonical-tail exit carries the same request-local report.
func (s *RecallService) searchProjectionOrTail(
	ctx context.Context,
	request memory.RecallRequest,
	currentSeq uint64,
	now time.Time,
) (memory.RecallResponse, error) {
	if s.projection == nil && request.MinScopeChangeSeq == 0 {
		return s.searchCanonicalTail(ctx, request, currentSeq, now)
	}

	hits, report, err := s.recallProjectionResult(ctx, request)
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

	if len(hits) == 0 && request.MinScopeChangeSeq == 0 {
		response, tailErr := s.searchCanonicalTail(ctx, request, currentSeq, now)
		if tailErr == nil {
			response.Retrieval = report
		}

		return response, tailErr
	}

	ids, byID, err := projectionRecallHits(request.Scope, hits)
	if err != nil {
		return memory.RecallResponse{}, err
	}

	if len(ids) == 0 {
		response := emptyRecallResponse(request.Scope, currentSeq)
		response.Retrieval = report

		return response, nil
	}

	records, err := s.canonical.LoadRecallRecords(ctx, request.Scope, ids)
	if err != nil {
		return memory.RecallResponse{}, err
	}

	response, err := s.responseFromProjection(ctx, request, records, byID, currentSeq, now)
	if err == nil {
		response.Retrieval = report
	}

	return response, err
}

//nolint:nestif // Optional reports retain the established certification gate before any reported search.
func (s *RecallService) recallProjectionResult(ctx context.Context, request memory.RecallRequest) (
	[]memory.RecallProjectionHit, *memory.RetrievalReport, error,
) {
	if projection, ok := s.projection.(memory.ReportedRecallProjection); ok {
		// A reported adapter still needs the established minimum-certification capability.
		if request.MinScopeChangeSeq != 0 {
			if _, consistent := s.projection.(memory.ConsistentRecallProjection); !consistent {
				return nil, nil, memory.RetryableError(memory.CodeConflict, "recall projection cannot certify consistency", nil)
			}
		}

		hits, report, err := projection.SearchRecallWithReport(ctx, request)
		if err != nil {
			return nil, nil, err
		}

		if report != nil {
			if err := report.Validate(); err != nil {
				return nil, nil, err
			}

			copyOfReport := *report
			report = &copyOfReport
		}

		return hits, report, nil
	}

	hits, err := s.recallProjectionHits(ctx, request)

	return hits, nil, err
}

func (s *RecallService) recallProjectionHits(ctx context.Context,
	request memory.RecallRequest) ([]memory.RecallProjectionHit,
	error) {
	if request.MinScopeChangeSeq == 0 {
		return s.projection.SearchRecall(ctx, request)
	}

	consistent, ok := s.projection.(memory.ConsistentRecallProjection)
	if !ok {
		return nil, memory.RetryableError(memory.CodeConflict, "recall projection cannot certify consistency", nil)
	}

	return consistent.SearchRecallConsistent(ctx, request)
}
