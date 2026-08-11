package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// LoadActiveRecallRecords returns a bounded active-head page suitable for the
// boundary compatibility adapter. It deliberately has no query parameter and
// never walks historical revisions.
func (r *CanonicalReader) LoadActiveRecallRecords(ctx context.Context, scope memory.Scope, limit uint32) ([]memory.RecallRecord, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit == 0 || limit > memory.MaxRecallCandidates {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "active canonical recall limit is invalid", nil)
	}
	active, err := r.store.ScanActiveMemory(ctx, memory.ActiveMemoryScanRequest{Scope: scope, Limit: limit})
	if err != nil {
		return nil, err
	}
	if len(active) == int(limit) {
		probe, probeErr := r.store.ScanActiveMemory(ctx, memory.ActiveMemoryScanRequest{Scope: scope, AfterItemID: active[len(active)-1].Item.ItemID, Limit: 1})
		if probeErr != nil {
			return nil, probeErr
		}
		if len(probe) != 0 {
			return nil, memory.PermanentError(memory.CodeLimitExceeded, "canonical boundary view exceeds the bounded active limit", nil)
		}
	}
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
