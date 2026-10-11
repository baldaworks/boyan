package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// LoadRecallRecords hydrates only the requested revision IDs. Missing,
// inactive, denied, or foreign candidates are skipped so a stale disposable
// projection cannot make an otherwise valid recall request fail.
func (r *CanonicalReader) LoadRecallRecords(
	ctx context.Context,
	scope memory.Scope,
	revisionIDs []string,
) ([]memory.RecallRecord, error) {
	if err := canonicalReaderContext(ctx); err != nil {
		return nil, err
	}

	if r == nil || r.store == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "canonical reader is unavailable", nil)
	}

	request := memory.CanonicalRevisionReadRequest{Scope: scope, RevisionIDs: revisionIDs}
	if err := request.Validate(); err != nil {
		return nil, err
	}

	result := make([]memory.RecallRecord, 0, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		record, found, err := r.loadRecallRecord(ctx, scope, revisionID, true)
		if err != nil {
			return nil, err
		}

		if found {
			result = append(result, record)
		}
	}

	return result, nil
}
