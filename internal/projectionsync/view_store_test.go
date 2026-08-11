package projectionsync

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

type boundedProjectionStore struct {
	CanonicalStore
	state     ScopeState
	active    []ActiveCanonicalMemory
	changes   []ScopeChange
	revisions map[string]MemoryRevision
}

func (s *boundedProjectionStore) LoadScopeState(context.Context, Scope) (ScopeState, error) {
	return s.state, nil
}

func (s *boundedProjectionStore) ScanActiveMemory(_ context.Context, request ActiveMemoryScanRequest) ([]ActiveCanonicalMemory, error) {
	page := make([]ActiveCanonicalMemory, 0, request.Limit)
	for _, active := range s.active {
		if request.AfterItemID != "" && active.Item.ItemID <= request.AfterItemID {
			continue
		}
		page = append(page, active)
		if uint32(len(page)) == request.Limit {
			break
		}
	}
	return page, nil
}

func (s *boundedProjectionStore) ScanScopeChanges(_ context.Context, _ Scope, after uint64, limit uint32) ([]ScopeChange, error) {
	changes := make([]ScopeChange, 0, limit)
	for _, change := range s.changes {
		if change.Sequence <= after {
			continue
		}
		changes = append(changes, change)
		if uint32(len(changes)) == limit {
			break
		}
	}
	return changes, nil
}

func (s *boundedProjectionStore) LoadCanonicalRevisions(_ context.Context, request CanonicalRevisionReadRequest) ([]MemoryRevision, error) {
	revisions := make([]MemoryRevision, 0, len(request.RevisionIDs))
	for _, id := range request.RevisionIDs {
		revision, ok := s.revisions[id]
		if !ok {
			return nil, PermanentError(CodeNotFound, "revision missing", nil)
		}
		revisions = append(revisions, revision)
	}
	return revisions, nil
}
