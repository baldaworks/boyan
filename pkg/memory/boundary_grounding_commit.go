package memory

import "time"

// BoundaryGroundingCommit supplies transient proof inputs for transactional
// validation of newly synthesized inherited bounds. Parents are canonical refs.
type BoundaryGroundingCommit struct {
	ProcessingAt time.Time
	Parents      map[string][]RevisionRef
}

func (m CanonicalMutation) validateBoundaryGrounding() error {
	if m.BoundaryGrounding == nil {
		for _, revision := range m.Revisions {
			if revision.GroundingBoundsVersion != 0 {
				return invalidDerived("inherited bounds require transactional proof")
			}
		}

		return nil
	}

	proof := m.BoundaryGrounding
	if proof.ProcessingAt.IsZero() || len(proof.Parents) != len(m.Revisions) {
		return invalidDerived("boundary grounding proof is incomplete")
	}

	for _, revision := range m.Revisions {
		refs, exists := proof.Parents[revision.RevisionID]
		if err := validateBoundaryProofRevision(revision, refs, exists); err != nil {
			return err
		}
	}

	return nil
}

func validateBoundaryProofRevision(revision MemoryRevision, refs []RevisionRef, exists bool) error {
	if !exists || revision.GroundingBoundsVersion != 1 || len(refs) != len(revision.GroundingParents) {
		return invalidDerived("boundary grounding proof does not match revision")
	}

	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return err
		}

		if _, duplicate := seen[ref.RevisionID]; duplicate {
			return invalidDerived("boundary grounding proof repeats a parent")
		}

		seen[ref.RevisionID] = struct{}{}
	}

	for _, id := range revision.GroundingParents {
		if _, exists := seen[id]; !exists {
			return invalidDerived("boundary grounding proof lacks semantic parent")
		}
	}

	return nil
}
