package memory

// Validate verifies non-empty, bounded, duplicate-free same-scope provenance.
func (p Provenance) Validate(scope Scope) error {
	if err := scope.Validate(); err != nil {
		return err
	}

	total := len(p.RawSources) + len(p.ParentRevisions)
	if total == 0 {
		return invalidDerived("derived provenance is required")
	}

	if total > MaxSourcesPerRevision {
		return limitExceeded("derived provenance exceeds the reference limit")
	}

	if err := validateRawSourceProvenance(scope, p.RawSources); err != nil {
		return err
	}

	parentSeen := make(map[RevisionRef]struct{}, len(p.ParentRevisions))
	for _, parent := range p.ParentRevisions {
		if err := parent.Validate(); err != nil {
			return err
		}

		if _, ok := parentSeen[parent]; ok {
			return invalidDerived("duplicate parent revision reference")
		}

		parentSeen[parent] = struct{}{}
	}

	return nil
}

func validateRawSourceProvenance(scope Scope, sources []SourceRef) error {
	rawSeen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if err := source.Validate(); err != nil {
			return err
		}

		if source.Scope != scope {
			return PermanentError(CodeScopeViolation, "raw source scope does not match the derived scope", nil)
		}

		key := source.Scope.Key + "\x00" + source.ExportID
		if _, ok := rawSeen[key]; ok {
			return invalidDerived("duplicate raw source reference")
		}

		rawSeen[key] = struct{}{}
	}

	return nil
}
