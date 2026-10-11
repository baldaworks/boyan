package projectionsync

import "context"

import . "github.com/baldaworks/boyan/pkg/memory"

func (b *ScopeViewBuilder) loadActive(
	ctx context.Context,
	scope Scope,
	after string,
	limit uint32,
) (
	[]ActiveProjectionRevision,
	bool,
	error,
) {
	active := make([]ActiveProjectionRevision, 0, limit)
	for uint32(len(active)) < limit {
		pageLimit := limit - uint32(len(active))
		if pageLimit > maxCanonicalReadRecords {
			pageLimit = maxCanonicalReadRecords
		}

		page, err := b.canonical.ScanActiveMemory(
			ctx,
			ActiveMemoryScanRequest{
				Scope:       scope,
				AfterItemID: after,
				Limit:       pageLimit,
			},
		)
		if err != nil {
			return nil, false, err
		}

		if len(page) == 0 {
			break
		}

		hydrated, err := b.hydrateActivePage(ctx, scope, page)
		if err != nil {
			return nil, false, err
		}

		active = append(active, hydrated...)

		after = page[len(page)-1].Item.ItemID
		if uint32(len(page)) < pageLimit {
			break
		}
	}

	if len(active) == 0 || uint32(len(active)) < limit {
		return active, false, nil
	}

	probe, err := b.canonical.ScanActiveMemory(
		ctx,
		ActiveMemoryScanRequest{Scope: scope, AfterItemID: active[len(active)-1].Item.ItemID, Limit: 1},
	)
	if err != nil {
		return nil, false, err
	}

	return active, len(probe) != 0, nil
}

func (b *ScopeViewBuilder) hydrateActivePage(
	ctx context.Context,
	scope Scope,
	page []ActiveCanonicalMemory,
) ([]ActiveProjectionRevision, error) {
	hydrated := make([]ActiveProjectionRevision, 0, len(page))

	ids := make([]string, 0, len(page))
	for _, item := range page {
		if item.Item.Scope != scope || item.RevisionID == "" {
			return nil, PermanentError(CodeScopeViolation, "canonical active memory has the wrong scope", nil)
		}

		ids = append(ids, item.RevisionID)
	}

	revisions, err := b.loadRevisionIDs(ctx, scope, ids)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]MemoryRevision, len(revisions))
	for _, revision := range revisions {
		byID[revision.RevisionID] = revision
	}

	for _, item := range page {
		revision, ok := byID[item.RevisionID]
		if !ok || revision.ItemID != item.Item.ItemID {
			return nil, PermanentError(CodeStoreFailure, "canonical active revision is missing", nil)
		}

		hydrated = append(hydrated, ActiveProjectionRevision{Item: item.Item, Revision: revision})
	}

	return hydrated, nil
}
