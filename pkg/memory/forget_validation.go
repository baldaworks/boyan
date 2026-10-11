package memory

// Validate verifies a supported forgetting operation kind.
func (k ForgetKind) Validate() error {
	switch k {
	case ForgetKindSource, ForgetKindScope:
		return nil
	default:
		return invalidDerived("unsupported forget kind")
	}
}

// Validate verifies a source-forgetting command.
func (c ForgetSourceCommand) Validate() error {
	if c.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported forget command schema version")
	}

	if err := c.Source.Validate(); err != nil {
		return err
	}

	if c.ForgottenAt.IsZero() {
		return invalidDerived("forget command timestamp is required")
	}

	return nil
}

// Validate verifies an exact-scope forgetting command.
func (c ForgetScopeCommand) Validate() error {
	if c.SchemaVersion != DerivedSchemaVersionV1 {
		return invalidDerived("unsupported forget command schema version")
	}

	if err := c.Scope.Validate(); err != nil {
		return err
	}

	if !isCanonicalID(c.RequestID) {
		return invalidDerived("scope forget request id is required")
	}

	if c.ForgottenAt.IsZero() {
		return invalidDerived("forget command timestamp is required")
	}

	return nil
}
