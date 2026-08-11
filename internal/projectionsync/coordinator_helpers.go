package projectionsync

import (
	"context"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (c *ProjectionCoordinator) loadRevisions(ctx context.Context, scope Scope, ids []string) ([]MemoryRevision, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	revisions := make([]MemoryRevision, 0, len(ids))
	for start := 0; start < len(ids); start += maxCanonicalReadRecords {
		end := start + maxCanonicalReadRecords
		if end > len(ids) {
			end = len(ids)
		}
		batch, err := c.canonical.LoadCanonicalRevisions(ctx, CanonicalRevisionReadRequest{Scope: scope, RevisionIDs: ids[start:end]})
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, batch...)
	}
	return revisions, nil
}

func (c *ProjectionCoordinator) currentTime() time.Time {
	if c.now == nil {
		return time.Now()
	}
	return c.now()
}

func uniqueRevisionIDs(changes []ScopeChange) []string {
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for _, change := range changes {
		for _, id := range change.RevisionIDs {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	return ids
}
