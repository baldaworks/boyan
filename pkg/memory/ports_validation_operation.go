package memory

// Validate verifies an operation lookup identity.
func (l OperationLookup) Validate() error {
	if l.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported operation lookup schema version")
	}

	if !isCanonicalID(l.OperationID) {
		return invalidDerived("operation lookup id is required")
	}

	if err := l.Stage.Validate(); err != nil {
		return err
	}

	return l.Scope.Validate()
}

// Validate verifies an operation lookup result against its request.
func (r OperationLookupResult) Validate(lookup OperationLookup) error {
	if err := lookup.Validate(); err != nil {
		return err
	}

	if !r.Found {
		if !operationOutcomeIsZero(r.Outcome) {
			return invalidDerived("missing operation lookup cannot contain an outcome")
		}

		return nil
	}

	if err := r.Outcome.Validate(); err != nil {
		return err
	}

	if r.Outcome.OperationID != lookup.OperationID || r.Outcome.Stage != lookup.Stage ||
		r.Outcome.Scope != lookup.Scope {
		return PermanentError(CodeScopeViolation, "operation lookup outcome does not match the request", nil)
	}

	return nil
}

func operationOutcomeIsZero(outcome OperationOutcome) bool {
	return outcome.SchemaVersion == "" && outcome.OperationID == "" && outcome.Stage == "" &&
		outcome.Scope == (Scope{}) && outcome.ScopeVersion == 0 && len(outcome.Revisions) == 0
}

// Validate verifies one append-only revision state transition.
func (t RevisionTransition) Validate() error {
	if err := t.Ref.Validate(); err != nil {
		return err
	}

	return ValidateRevisionStateTransition(t.From, t.To)
}
