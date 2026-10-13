package memory

import "slices"

// Validate verifies immutable revision metadata.
func (r MemoryRevision) Validate() error {
	if r.SchemaVersion != MemorySchemaVersion || !isCanonicalID(r.RevisionID) ||
		!isCanonicalID(r.ItemID) ||
		r.Revision == 0 {
		return invalidDerived("memory revision identity is invalid")
	}

	if err := r.validateGroundingMetadata(); err != nil {
		return err
	}

	if err := r.Temporal.Validate(); err != nil {
		return err
	}

	if err := validateMemoryRevisionEvidence(r.Evidence); err != nil {
		return err
	}

	if err := r.validateParents(); err != nil {
		return err
	}

	if err := validateSensitivityRetention(r.Sensitivity, r.Retention); err != nil {
		return err
	}

	return r.Payload.Validate()
}

func validateUniqueMemoryRefs(ids []string, name string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !isCanonicalID(id) {
			return invalidDerived(name + " is invalid")
		}

		if _, exists := seen[id]; exists {
			return invalidDerived(name + " is duplicated")
		}

		seen[id] = struct{}{}
	}

	return nil
}

func (r MemoryRevision) validateParents() error {
	if err := validateUniqueMemoryRefs(r.Parents, "memory revision parent"); err != nil {
		return err
	}

	for _, parent := range r.Parents {
		if parent == r.RevisionID {
			return invalidDerived("memory revision cannot parent itself")
		}
	}

	return nil
}

func validateMemoryRevisionEvidence(evidence []EvidenceRef) error {
	if len(evidence) == 0 || len(evidence) > MaxSourcesPerRevision {
		return invalidDerived("memory revision evidence is invalid")
	}

	if err := validateEvidenceRefs(evidence); err != nil {
		return err
	}

	return nil
}

func (r MemoryRevision) validateGroundingMetadata() error {
	if r.GroundingBoundsVersion == 0 {
		if len(r.GroundingParents) != 0 {
			return invalidDerived("unversioned grounding metadata")
		}

		return nil
	}

	if r.GroundingBoundsVersion != 1 || len(r.GroundingParents) == 0 || len(r.GroundingParents) > MaxSourcesPerRevision {
		return invalidDerived("inherited grounding metadata is invalid")
	}

	if err := validateUniqueMemoryRefs(r.GroundingParents, "grounding parent"); err != nil {
		return err
	}

	for _, parent := range r.GroundingParents {
		if !slices.Contains(r.Parents, parent) {
			return invalidDerived("grounding parent is not provenance")
		}
	}

	return nil
}
