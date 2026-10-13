package canonicalops

import memory "github.com/baldaworks/boyan/pkg/memory"

func inheritBoundaryRevisions(request memory.CommitRequest, view memory.CanonicalBoundaryView,
	mutation *memory.CanonicalMutation) error {
	proof := &memory.BoundaryGroundingCommit{ProcessingAt: request.ProcessingAt,
		Parents: make(map[string][]memory.RevisionRef)}

	semantic := make(map[string][]memory.RevisionRef, len(mutation.Revisions))
	for _, scenario := range request.Scenarios {
		semantic[scenario.Meta.RevisionID] = semanticBoundaryParents(scenario.Meta)
	}

	for _, profile := range request.Profiles {
		semantic[profile.Meta.RevisionID] = semanticBoundaryParents(profile.Meta)
	}

	for index := range mutation.Revisions {
		revision := &mutation.Revisions[index]
		refs := semantic[revision.RevisionID]
		parents := make([]memory.BoundaryParentMetadata, 0, len(refs))

		canonical := make([]memory.RevisionRef, 0, len(refs))
		for _, ref := range refs {
			parent, ok := view.Boundary.Parents[ref]
			if !ok {
				return memory.PermanentError(memory.CodeInvalidDerived, "boundary parent metadata is unavailable", nil)
			}

			parents = append(parents, parent)
			canonical = append(canonical, parent.CanonicalRef)
		}

		inherited, err := memory.InheritBoundaryMetadata(request.ProcessingAt, parents)
		if err != nil {
			return err
		}

		if !memory.TemporalEligibleAt(inherited.Temporal, request.ProcessingAt) {
			return memory.PermanentError(memory.CodeInvalidDerived, "boundary inherited interval is not eligible", nil)
		}

		revision.Temporal = inherited.Temporal
		revision.Sensitivity = inherited.Sensitivity
		revision.Retention = inherited.Retention
		revision.GroundingBoundsVersion = 1
		revision.GroundingParents = revisionIDs(canonical)
		proof.Parents[revision.RevisionID] = canonical
	}

	mutation.BoundaryGrounding = proof

	return nil
}

func semanticBoundaryParents(meta memory.RevisionMeta) []memory.RevisionRef {
	refs := make([]memory.RevisionRef, 0, len(meta.Provenance.ParentRevisions))
	for _, ref := range meta.Provenance.ParentRevisions {
		if meta.Supersedes == nil || ref != *meta.Supersedes {
			refs = append(refs, ref)
		}
	}

	return refs
}
