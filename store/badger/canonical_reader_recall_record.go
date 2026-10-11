package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *CanonicalReader) loadRecallRecord(
	ctx context.Context,
	scope memory.Scope,
	revisionID string,
	activeOnly bool,
) (memory.RecallRecord, bool, error) {
	view, found, err := r.loadCanonicalView(ctx, scope, revisionID, activeOnly)
	if err != nil || !found {
		return memory.RecallRecord{}, found, err
	}

	text, err := r.store.LoadPayload(ctx, view.revision.Payload)
	if err != nil {
		if isCanonicalMissing(err) {
			return memory.RecallRecord{}, false, nil
		}

		return memory.RecallRecord{}, false, err
	}

	record := buildRecallRecord(scope, view, text)

	state, err := r.store.LoadScopeState(ctx, scope)
	if err != nil {
		return memory.RecallRecord{}, false, err
	}

	record.ScopeChangeSeq = state.ChangeSeq
	if err := populateRecallRecordSources(scope, view, &record); err != nil {
		return memory.RecallRecord{}, false, err
	}

	return record, true, nil
}
