package memory

// Validate verifies one bounded canonical mutation request.
func (m CanonicalMutation) Validate() error {
	if m.SchemaVersion != CanonicalSchemaVersionV1 {
		return invalidDerived("canonical mutation schema version is invalid")
	}

	if err := m.Scope.Validate(); err != nil {
		return err
	}

	if err := validateCanonicalMutationOperation(m.Operation); err != nil {
		return err
	}

	if count := canonicalMutationRecordCount(m); count == 0 || count > maxCanonicalMutationRecords {
		return invalidDerived("canonical mutation record count is invalid")
	}

	if m.TurnIdentity != nil {
		if err := m.TurnIdentity.Validate(m.Scope); err != nil {
			return err
		}
	}

	if err := m.validateBoundaryGrounding(); err != nil {
		return err
	}

	return m.validateRecords()
}

func (m CanonicalMutation) validateRecords() error {
	if err := validateCanonicalMutationSources(m.Scope, m.Sources); err != nil {
		return err
	}

	if err := validateCanonicalMutationMessages(m.Messages); err != nil {
		return err
	}

	if err := validateCanonicalMutationItems(m.Scope, m.Items); err != nil {
		return err
	}

	if err := validateCanonicalMutationRevisions(m.Revisions); err != nil {
		return err
	}

	if err := validateCanonicalMutationLifecycle(m.Lifecycle); err != nil {
		return err
	}

	if err := validateCanonicalMutationHeads(m.Heads); err != nil {
		return err
	}

	if err := validateCanonicalMutationDelivery(m.Delivery); err != nil {
		return err
	}

	return validateCanonicalMutationPayloads(m.Payloads)
}
func validateCanonicalMutationOperation(op OperationRecord) error {
	if !isCanonicalID(op.OperationID) || !isCanonicalID(op.Fingerprint) ||
		op.CommittedAt.IsZero() {
		return invalidDerived("canonical mutation operation is invalid")
	}

	return validateUniqueCanonicalIDs(op.Outcome, "canonical operation outcome")
}

func canonicalMutationRecordCount(m CanonicalMutation) int {
	return len(m.Sources) +
		len(m.Messages) +
		len(m.Items) +
		len(m.Revisions) +
		len(m.Lifecycle) +
		len(m.Heads) +
		len(m.Delivery) +
		len(m.Payloads)
}
