package memory

// Validate checks one canonical mutation outcome.
func (o CanonicalMutationOutcome) Validate() error {
	if o.ScopeVersion == 0 || o.ChangeSeq == 0 {
		return invalidDerived("canonical mutation outcome is invalid")
	}
	for _, revisionID := range o.RevisionIDs {
		if !isCanonicalID(revisionID) {
			return invalidDerived("canonical mutation outcome revision is invalid")
		}
	}
	return nil
}
