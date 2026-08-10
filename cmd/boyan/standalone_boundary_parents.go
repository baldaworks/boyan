package main

import (
	"context"
	"errors"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var errBoundaryEvidenceRequired = errors.New("boundary commit requires grounded parent evidence")

func (s *canonicalBoundaryStore) translateBoundaryParents(ctx context.Context, scope memory.Scope, mapping map[memory.RevisionRef]memory.RevisionRef, request memory.CommitRequest) (map[string][]memory.RevisionRef, map[string][]memory.EvidenceRef, error) {
	parentSets := make(map[string][]memory.RevisionRef, len(request.Scenarios)+len(request.Profiles))
	needed := make(map[string]memory.RevisionRef)
	translate := func(ref memory.RevisionRef) (memory.RevisionRef, error) {
		if canonical, ok := mapping[ref]; ok {
			return canonical, nil
		}
		if err := ref.Validate(); err != nil {
			return memory.RevisionRef{}, err
		}
		return ref, nil
	}
	addParents := func(revisionID string, parents []memory.RevisionRef) error {
		translated := make([]memory.RevisionRef, 0, len(parents))
		for _, parent := range parents {
			canonical, err := translate(parent)
			if err != nil {
				return err
			}
			translated = appendUniqueRefs(translated, canonical)
			needed[canonical.RevisionID] = canonical
		}
		parentSets[revisionID] = translated
		return nil
	}
	for _, scenario := range request.Scenarios {
		if err := addParents(scenario.Meta.RevisionID, scenario.Meta.Provenance.ParentRevisions); err != nil {
			return nil, nil, err
		}
	}
	for _, profile := range request.Profiles {
		if err := addParents(profile.Meta.RevisionID, profile.Meta.Provenance.ParentRevisions); err != nil {
			return nil, nil, err
		}
	}
	if len(needed) == 0 {
		return nil, nil, fmt.Errorf("boundary commit requires parent revisions")
	}
	loaded, err := loadCanonicalRevisionsByID(ctx, s.store, scope, needed)
	if err != nil {
		return nil, nil, err
	}
	evidenceSets := make(map[string][]memory.EvidenceRef, len(parentSets))
	for revisionID, parents := range parentSets {
		seen := make(map[string]struct{})
		evidence := make([]memory.EvidenceRef, 0, len(parents))
		for _, parent := range parents {
			revision, ok := loaded[parent.RevisionID]
			if !ok {
				return nil, nil, fmt.Errorf("canonical parent revision %s is unavailable", parent.RevisionID)
			}
			for _, ref := range revision.Evidence {
				key := evidenceKey(ref)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				evidence = append(evidence, ref)
			}
		}
		if len(evidence) == 0 {
			return nil, nil, fmt.Errorf("boundary commit parent evidence is empty")
		}
		if len(evidence) > memory.MaxSourcesPerRevision {
			return nil, nil, fmt.Errorf("boundary commit evidence exceeds %d references", memory.MaxSourcesPerRevision)
		}
		evidenceSets[revisionID] = evidence
	}
	return parentSets, evidenceSets, nil
}

func (s *canonicalBoundaryStore) lookupOperationRevisions(ctx context.Context, scope memory.Scope, revisionIDs []string) ([]memory.RevisionRef, error) {
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
		compatibility, found, err := s.reader.LoadCanonicalCompatibility(ctx, scope, revisionID)
		if err != nil {
			return nil, err
		}
		if found {
			refs = append(refs, memory.RevisionRef{
				ItemID:     compatibility.LegacyItemID,
				RevisionID: compatibility.LegacyRevisionID,
			})
			continue
		}
		revision, ok := loaded[revisionID]
		if !ok {
			return nil, fmt.Errorf("canonical replay revision %s is unavailable", revisionID)
		}
		refs = append(refs, memory.RevisionRef{ItemID: revision.ItemID, RevisionID: revision.RevisionID})
	}
	return refs, nil
}
