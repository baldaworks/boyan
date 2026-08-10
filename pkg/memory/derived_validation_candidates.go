package memory

// Validate verifies syntactically grounded atom candidate output.
func (c AtomCandidate) Validate() error {
	if err := c.Category.Validate(); err != nil {
		return err
	}
	if _, err := validateDerivedText("atom candidate text", c.Text); err != nil {
		return err
	}
	if err := c.Relation.Validate(); err != nil {
		return err
	}
	switch c.Relation {
	case CandidateRelationNew:
		if c.Target != nil {
			return invalidDerived("new atom candidate cannot have a target")
		}
	case CandidateRelationCoexist, CandidateRelationSupersede:
		if c.Target == nil {
			return invalidDerived("related atom candidate requires a target")
		}
		if err := c.Target.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate verifies syntactically grounded scenario candidate output.
func (c ScenarioCandidate) Validate() error {
	if _, err := validateDerivedKey("scenario candidate topic key", c.TopicKey); err != nil {
		return err
	}
	if _, err := validateDerivedText("scenario candidate title", c.Title); err != nil {
		return err
	}
	if _, err := validateDerivedText("scenario candidate summary", c.Summary); err != nil {
		return err
	}
	if len(c.Atoms) == 0 {
		return invalidDerived("scenario candidate requires an atom reference")
	}
	if err := validateRevisionRefs(c.Atoms); err != nil {
		return err
	}
	if c.Supersedes != nil {
		if err := c.Supersedes.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate verifies syntactically grounded profile candidate output.
func (c ProfileCandidate) Validate() error {
	if err := c.Disposition.Validate(); err != nil {
		return err
	}
	if c.Disposition == ProfileDispositionSkip {
		if c.Summary != "" || len(c.Atoms) != 0 || len(c.Scenarios) != 0 || c.Supersedes != nil {
			return invalidDerived("skipped profile candidate cannot contain a revision")
		}
		return nil
	}
	if _, err := validateDerivedText("profile candidate summary", c.Summary); err != nil {
		return err
	}
	if len(c.Atoms)+len(c.Scenarios) == 0 {
		return invalidDerived("profile candidate requires a parent revision")
	}
	if len(c.Atoms)+len(c.Scenarios) > MaxSourcesPerRevision {
		return limitExceeded("profile candidate exceeds the parent reference limit")
	}
	if err := validateRevisionRefs(c.Atoms); err != nil {
		return err
	}
	if err := validateRevisionRefs(c.Scenarios); err != nil {
		return err
	}
	if c.Supersedes != nil {
		if err := c.Supersedes.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate verifies an idempotent processing outcome.
func (o OperationOutcome) Validate() error {
	if o.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported operation outcome schema version")
	}
	if !isCanonicalID(o.OperationID) {
		return invalidDerived("operation id is required")
	}
	if err := o.Stage.Validate(); err != nil {
		return err
	}
	if err := o.Scope.Validate(); err != nil {
		return err
	}
	if o.ScopeVersion == 0 {
		return invalidDerived("scope version is required")
	}
	if len(o.Revisions) > MaxCandidateCount {
		return limitExceeded("operation outcome exceeds the revision limit")
	}
	return validateRevisionRefs(o.Revisions)
}

// Validate verifies both independently committed boundary stages.
func (o BoundaryOutcome) Validate() error {
	if o.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported boundary outcome schema version")
	}
	if err := o.Scope.Validate(); err != nil {
		return err
	}
	if err := o.Scenarios.Validate(); err != nil {
		return err
	}
	if err := o.Profile.Validate(); err != nil {
		return err
	}
	if o.Scenarios.Scope != o.Scope || o.Profile.Scope != o.Scope {
		return PermanentError(CodeScopeViolation, "boundary outcome stage scope does not match the boundary", nil)
	}
	if o.Scenarios.Stage != OperationStageScenarios || o.Profile.Stage != OperationStageProfile {
		return invalidDerived("boundary outcome contains the wrong processing stages")
	}
	if o.Profile.ScopeVersion <= o.Scenarios.ScopeVersion {
		return invalidDerived("profile stage must commit after the scenario stage")
	}
	return nil
}
