package memory

func validateScopeSnapshotRevisions(scope Scope, atoms []Atom, scenarios []Scenario, profiles []Profile) error {
	revisionSeen := make(map[string]struct{}, len(atoms)+len(scenarios)+len(profiles))
	for _, atom := range atoms {
		if err := atom.Validate(); err != nil {
			return err
		}
		if err := validateSnapshotRevision(scope, atom.Meta, revisionSeen); err != nil {
			return err
		}
	}
	for _, scenario := range scenarios {
		if err := scenario.Validate(); err != nil {
			return err
		}
		if err := validateSnapshotRevision(scope, scenario.Meta, revisionSeen); err != nil {
			return err
		}
	}
	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return err
		}
		if err := validateSnapshotRevision(scope, profile.Meta, revisionSeen); err != nil {
			return err
		}
	}
	return nil
}

func validateSnapshotRevision(scope Scope, meta RevisionMeta, seen map[string]struct{}) error {
	if meta.Scope != scope {
		return PermanentError(CodeScopeViolation, "derived revision scope does not match snapshot scope", nil)
	}
	if _, ok := seen[meta.RevisionID]; ok {
		return invalidDerived("scope snapshot contains duplicate revision identity")
	}
	seen[meta.RevisionID] = struct{}{}
	return nil
}
