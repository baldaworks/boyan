package canonicalops

import memory "github.com/baldaworks/boyan/pkg/memory"

func validateBoundaryGroundingAt(request memory.CommitRequest, snapshot memory.BoundarySnapshot) error {
	eligible := make(map[memory.RevisionRef]struct{})
	for _, atom := range snapshot.Eligible.Atoms {
		eligible[memory.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}] = struct{}{}
	}

	for _, scenario := range snapshot.Eligible.Scenarios {
		eligible[memory.RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}] = struct{}{}
	}

	for _, scenario := range request.Scenarios {
		if err := validateEligibleParents(scenario.Meta, eligible); err != nil {
			return err
		}
	}

	for _, profile := range request.Profiles {
		if err := validateEligibleParents(profile.Meta, eligible); err != nil {
			return err
		}
	}

	return nil
}

func validateEligibleParents(meta memory.RevisionMeta, eligible map[memory.RevisionRef]struct{}) error {
	for _, parent := range meta.Provenance.ParentRevisions {
		if meta.Supersedes != nil && parent == *meta.Supersedes {
			continue
		}

		if _, ok := eligible[parent]; !ok {
			return memory.PermanentError(memory.CodeInvalidDerived, "boundary grounding parent is not eligible", nil)
		}
	}

	return nil
}
