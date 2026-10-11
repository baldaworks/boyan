package forgetops

import (
	"context"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/badger"
)

func canonicalRevisionRefs(
	ctx context.Context,
	store *badger.CanonicalStore,
	scope memory.Scope,
	revisionIDs []string,
) ([]memory.RevisionRef, error) {
	needed := make(map[string]memory.RevisionRef, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		needed[revisionID] = memory.RevisionRef{RevisionID: revisionID}
	}

	loaded, err := loadCanonicalRevisionsByID(ctx, store, scope, needed)
	if err != nil {
		return nil, err
	}

	refs := make([]memory.RevisionRef, 0, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		revision, ok := loaded[revisionID]
		if !ok {
			return nil, fmt.Errorf("canonical revision %s is unavailable", revisionID)
		}

		refs = append(refs, memory.RevisionRef{ItemID: revision.ItemID, RevisionID: revision.RevisionID})
	}

	return refs, nil
}
