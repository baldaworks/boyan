package memory

func (r RecallRecord) validateRecallLegacyCompatibility() error {
	if r.LegacyKind == nil {
		return nil
	}
	if err := r.LegacyKind.Validate(); err != nil {
		return err
	}
	if !isCanonicalID(r.LegacyItemID) || !isCanonicalID(r.LegacyRevisionID) || !isCanonicalID(r.LegacyOperationID) {
		return invalidDerived("recall compatibility identity is invalid")
	}
	if r.LegacySupersedes != nil {
		if err := r.LegacySupersedes.Validate(); err != nil {
			return err
		}
	}
	if len(r.LegacyParents) > MaxSourcesPerRevision {
		return limitExceeded("recall compatibility parent bound exceeded")
	}
	for _, parent := range r.LegacyParents {
		if err := parent.Validate(); err != nil {
			return err
		}
	}
	return nil
}
