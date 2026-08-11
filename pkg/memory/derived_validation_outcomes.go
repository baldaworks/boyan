package memory

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
