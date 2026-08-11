package memory

// Validate verifies one durable projection checkpoint.
func (c ScopeCheckpoint) Validate() error {
	if c.SchemaVersion != ScopeCheckpointSchemaVersion {
		return invalidDerived("scope checkpoint schema version is invalid")
	}
	if err := c.Scope.Validate(); err != nil {
		return err
	}
	if err := c.Kind.Validate(); err != nil {
		return err
	}
	if !isCanonicalID(c.CheckpointID) || c.OccurredAt.IsZero() || c.UpdatedAt.IsZero() {
		return invalidDerived("scope checkpoint identity or timestamp is invalid")
	}
	if c.Kind != ScopeCheckpointToken && c.TokenCount != 0 {
		return invalidDerived("non-token checkpoint contains a token count")
	}
	return nil
}
