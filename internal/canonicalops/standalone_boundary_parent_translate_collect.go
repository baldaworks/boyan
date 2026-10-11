package canonicalops

import memory "github.com/baldaworks/boyan/pkg/memory"

func collectBoundaryParentSets(
	mapping map[memory.RevisionRef]memory.RevisionRef,
	request memory.CommitRequest,
) (map[string][]memory.RevisionRef, map[string]memory.RevisionRef, error) {
	parentSets := make(map[string][]memory.RevisionRef, len(request.Scenarios)+len(request.Profiles))

	needed := make(map[string]memory.RevisionRef)
	for _, scenario := range request.Scenarios {
		if err := addBoundaryParentSet(
			parentSets,
			needed,
			mapping,
			scenario.Meta.RevisionID,
			scenario.Meta.Provenance.ParentRevisions,
		); err != nil {
			return nil, nil, err
		}
	}

	for _, profile := range request.Profiles {
		if err := addBoundaryParentSet(
			parentSets,
			needed,
			mapping,
			profile.Meta.RevisionID,
			profile.Meta.Provenance.ParentRevisions,
		); err != nil {
			return nil, nil, err
		}
	}

	return parentSets, needed, nil
}

func addBoundaryParentSet(
	parentSets map[string][]memory.RevisionRef,
	needed map[string]memory.RevisionRef,
	mapping map[memory.RevisionRef]memory.RevisionRef,
	revisionID string,
	parents []memory.RevisionRef,
) error {
	translated := make([]memory.RevisionRef, 0, len(parents))
	for _, parent := range parents {
		canonical, err := translateBoundaryParent(parent, mapping)
		if err != nil {
			return err
		}

		translated = appendUniqueRefs(translated, canonical)
		needed[canonical.RevisionID] = canonical
	}

	parentSets[revisionID] = translated

	return nil
}

func translateBoundaryParent(
	ref memory.RevisionRef,
	mapping map[memory.RevisionRef]memory.RevisionRef,
) (memory.RevisionRef, error) {
	if canonical, ok := mapping[ref]; ok {
		return canonical, nil
	}

	if err := ref.Validate(); err != nil {
		return memory.RevisionRef{}, err
	}

	return ref, nil
}
