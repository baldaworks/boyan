package memory

func (p CanonicalCompatibilityPayload) validateCompatibilityParents() error {
	if len(p.LegacyParents) > MaxSourcesPerRevision {
		return limitExceeded("canonical compatibility parent bound exceeded")
	}

	for _, parent := range p.LegacyParents {
		if err := parent.Validate(); err != nil {
			return err
		}
	}

	return nil
}
