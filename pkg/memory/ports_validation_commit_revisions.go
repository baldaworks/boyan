package memory

func validateCommitSources(r CommitRequest) error {
	for _, source := range r.Sources {
		if err := source.Validate(); err != nil {
			return err
		}
		if source.Ref.Scope != r.Scope {
			return PermanentError(CodeScopeViolation, "commit source scope does not match the operation", nil)
		}
	}
	return nil
}

func validateCommitRevisions(r CommitRequest) error {
	created := make(map[RevisionRef]struct{}, len(r.Atoms)+len(r.Scenarios)+len(r.Profiles))
	for _, atom := range r.Atoms {
		if err := atom.Validate(); err != nil {
			return err
		}
		if err := validateCommitRevision(r, atom.Meta, created); err != nil {
			return err
		}
	}
	for _, scenario := range r.Scenarios {
		if err := scenario.Validate(); err != nil {
			return err
		}
		if err := validateCommitRevision(r, scenario.Meta, created); err != nil {
			return err
		}
	}
	for _, profile := range r.Profiles {
		if err := profile.Validate(); err != nil {
			return err
		}
		if err := validateCommitRevision(r, profile.Meta, created); err != nil {
			return err
		}
	}
	return nil
}
