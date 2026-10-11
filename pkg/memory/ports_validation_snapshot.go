package memory

// Validate verifies one bounded exact-scope Store snapshot.
func (s ScopeSnapshot) Validate(maxItems int) error {
	if s.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported scope snapshot schema version")
	}

	if err := s.Scope.Validate(); err != nil {
		return err
	}

	if maxItems < 1 || maxItems > MaxSnapshotItems {
		return limitExceeded("snapshot validation limit is outside the allowed range")
	}

	if len(s.Sources)+len(s.Atoms)+len(s.Scenarios)+len(s.Profiles) > maxItems {
		return limitExceeded("scope snapshot exceeds the item limit")
	}

	if err := validateScopeSnapshotSources(s.Scope, s.Sources); err != nil {
		return err
	}

	return validateScopeSnapshotRevisions(s.Scope, s.Atoms, s.Scenarios, s.Profiles)
}
