package canonicalops

import (
	"context"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *CanonicalBoundaryStore) lookupOperationRevisions(ctx context.Context, scope memory.Scope, revisionIDs []string) ([]memory.RevisionRef, error) {
	if len(revisionIDs) == 0 {
		return nil, nil
	}
	needed := make(map[string]memory.RevisionRef, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		needed[revisionID] = memory.RevisionRef{RevisionID: revisionID}
	}
	loaded, err := loadCanonicalRevisionsByID(ctx, s.store, scope, needed)
	if err != nil {
		return nil, err
	}
	refs := make([]memory.RevisionRef, 0, len(revisionIDs))
	for _, revisionID := range revisionIDs {
		ref, err := s.lookupOperationRevision(ctx, scope, revisionID, loaded)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (s *CanonicalBoundaryStore) lookupOperationRevision(ctx context.Context, scope memory.Scope, revisionID string, loaded map[string]memory.MemoryRevision) (memory.RevisionRef, error) {
	compatibility, found, err := s.reader.LoadCanonicalCompatibility(ctx, scope, revisionID)
	if err != nil {
		return memory.RevisionRef{}, err
	}
	if found {
		return memory.RevisionRef{
			ItemID:     compatibility.LegacyItemID,
			RevisionID: compatibility.LegacyRevisionID,
		}, nil
	}
	revision, ok := loaded[revisionID]
	if !ok {
		return memory.RevisionRef{}, fmt.Errorf("canonical replay revision %s is unavailable", revisionID)
	}
	return memory.RevisionRef{ItemID: revision.ItemID, RevisionID: revision.RevisionID}, nil
}
