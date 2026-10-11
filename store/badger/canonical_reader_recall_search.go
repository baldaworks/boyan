package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// SearchRecallTail returns a bounded active-head page. Text matching and
// ranking remain in RecallService; this adapter never scans complete history.
func (r *CanonicalReader) SearchRecallTail(
	ctx context.Context,
	request memory.RecallRequest,
	limit uint32,
) ([]memory.RecallRecord, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return nil, err
	}

	if r == nil || r.store == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "canonical reader is unavailable", nil)
	}

	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return nil, err
	}

	if limit == 0 || limit > memory.MaxRecallCandidates {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "canonical recall tail limit is invalid", nil)
	}

	active, err := r.store.ScanActiveMemory(ctx, memory.ActiveMemoryScanRequest{Scope: normalized.Scope, Limit: limit})
	if err != nil {
		return nil, err
	}

	return r.loadActiveRecallRecords(ctx, normalized.Scope, active)
}

func (r *CanonicalReader) loadActiveRecallRecords(
	ctx context.Context,
	scope memory.Scope,
	active []memory.ActiveCanonicalMemory,
) ([]memory.RecallRecord, error) {
	result := make([]memory.RecallRecord, 0, len(active))
	for _, memory := range active {
		record, found, loadErr := r.loadRecallRecord(ctx, scope, memory.RevisionID, true)
		if loadErr != nil {
			return nil, loadErr
		}

		if found {
			result = append(result, record)
		}
	}

	return result, nil
}
