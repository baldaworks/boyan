package memory

// Validate checks one canonical compatibility payload for one exact scope.
func (p CanonicalCompatibilityPayload) Validate(scope Scope) error {
	if p.SchemaVersion != CanonicalCompatibilitySchemaVersion {
		return invalidDerived("canonical compatibility payload schema version is invalid")
	}
	if err := p.Kind.Validate(); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if err := p.validateCompatibilityIdentity(); err != nil {
		return err
	}
	if err := p.validateCompatibilityParents(); err != nil {
		return err
	}
	if p.Text == "" {
		return invalidDerived("canonical compatibility text is required")
	}
	return p.validateCompatibilityKindFields()
}
