package canonicalops

import (
	"context"
	"errors"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var errBoundaryEvidenceRequired = errors.New("boundary commit requires grounded parent evidence")

func (s *CanonicalBoundaryStore) translateBoundaryParents(ctx context.Context, scope memory.Scope, mapping map[memory.RevisionRef]memory.RevisionRef, request memory.CommitRequest) (map[string][]memory.RevisionRef, map[string][]memory.EvidenceRef, error) {
	parentSets, needed, err := collectBoundaryParentSets(mapping, request)
	if err != nil {
		return nil, nil, err
	}
	if len(needed) == 0 {
		return nil, nil, fmt.Errorf("boundary commit requires parent revisions")
	}
	loaded, err := loadCanonicalRevisionsByID(ctx, s.store, scope, needed)
	if err != nil {
		return nil, nil, err
	}
	evidenceSets, err := buildBoundaryParentEvidenceSets(parentSets, loaded)
	if err != nil {
		return nil, nil, err
	}
	return parentSets, evidenceSets, nil
}
