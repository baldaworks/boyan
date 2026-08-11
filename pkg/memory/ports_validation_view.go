package memory

// Validate verifies a bounded same-scope model view.
func (v ScopeView) Validate(maxItems int) error {
	return (ScopeSnapshot{
		SchemaVersion: v.SchemaVersion,
		Scope:         v.Scope,
		Version:       v.Version,
		Atoms:         v.Atoms,
		Scenarios:     v.Scenarios,
		Profiles:      v.Profiles,
	}).Validate(maxItems)
}
