package memoryread

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Search executes one exact-scope recall request.
func (s *RecallService) Search(ctx context.Context, request memory.RecallRequest) (memory.RecallResponse, error) {
	if s == nil || s.canonical == nil {
		return memory.RecallResponse{}, memory.PermanentError(memory.CodeDisabled, "recall service is unavailable", nil)
	}

	if ctx == nil {
		return memory.RecallResponse{}, memory.PermanentError(memory.CodeInvalidQuery, "recall context is required", nil)
	}

	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return memory.RecallResponse{}, err
	}

	now := s.recallNow()

	currentSeq, err := s.currentRecallScopeChangeSeq(ctx, normalized.Scope, normalized.MinScopeChangeSeq)
	if err != nil {
		return memory.RecallResponse{}, err
	}

	return s.searchProjectionOrTail(ctx, normalized, currentSeq, now)
}

func (s *RecallService) searchCanonicalTail(
	ctx context.Context,
	request memory.RecallRequest,
	currentSeq uint64,
	now time.Time,
) (
	memory.RecallResponse,
	error,
) {
	records, err := s.canonical.SearchRecallTail(ctx, request, recallTailLimit(request.Limit))
	if err != nil {
		return memory.RecallResponse{}, err
	}

	return s.responseFromRecords(request, records, currentSeq, now)
}
