package memory

func (p CanonicalCompatibilityPayload) validateCompatibilityIdentity() error {
	if !isCanonicalID(p.LegacyItemID) || !isCanonicalID(p.LegacyRevisionID) ||
		!isCanonicalID(p.LegacyOperationID) {
		return invalidDerived("canonical compatibility identity is invalid")
	}

	if p.Supersedes != nil {
		return p.Supersedes.Validate()
	}

	return nil
}
