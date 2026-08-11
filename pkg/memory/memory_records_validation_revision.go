package memory

// Validate verifies immutable revision metadata.
func (r MemoryRevision) Validate() error {
	if r.SchemaVersion != MemorySchemaVersion || !isCanonicalID(r.RevisionID) || !isCanonicalID(r.ItemID) || r.Revision == 0 {
		return invalidDerived("memory revision identity is invalid")
	}
	if err := r.Temporal.Validate(); err != nil {
		return err
	}
	if len(r.Evidence) == 0 || len(r.Evidence) > MaxSourcesPerRevision {
		return invalidDerived("memory revision evidence is invalid")
	}
	if err := validateEvidenceRefs(r.Evidence); err != nil {
		return err
	}
	if err := validateUniqueMemoryRefs(r.Parents, "memory revision parent"); err != nil {
		return err
	}
	for _, parent := range r.Parents {
		if parent == r.RevisionID {
			return invalidDerived("memory revision cannot parent itself")
		}
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
