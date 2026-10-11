package projectionsync

import "context"

import . "github.com/baldaworks/boyan/pkg/memory"

func (b *ScopeViewBuilder) loadChanges(
	ctx context.Context,
	scope Scope,
	after uint64,
	limit uint32,
) (
	[]MemoryRevision,
	error,
) {
	if limit == 0 {
		return nil, nil
	}

	changes, err := b.canonical.ScanScopeChanges(ctx, scope, after, limit)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(changes))

	seen := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		for _, id := range change.RevisionIDs {
			if _, exists := seen[id]; exists {
				continue
			}

			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}

	return b.loadRevisionIDs(ctx, scope, ids)
}

func (b *ScopeViewBuilder) loadRevisionIDs(ctx context.Context, scope Scope, ids []string) ([]MemoryRevision, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	revisions := make([]MemoryRevision, 0, len(ids))
	for start := 0; start < len(ids); start += maxCanonicalReadRecords {
		end := start + maxCanonicalReadRecords
		if end > len(ids) {
			end = len(ids)
		}

		batch, err := b.canonical.LoadCanonicalRevisions(
			ctx,
			CanonicalRevisionReadRequest{
				Scope:       scope,
				RevisionIDs: ids[start:end],
			},
		)
		if err != nil {
			return nil, err
		}

		revisions = append(revisions, batch...)
	}

	return revisions, nil
}
