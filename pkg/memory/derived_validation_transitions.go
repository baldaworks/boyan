package memory

// ValidateRevisionStateTransition verifies an append-only revision lifecycle.
func ValidateRevisionStateTransition(from, to RevisionState) error {
	if to.Validate() != nil {
		return invalidDerived("unsupported target revision state")
	}
	if from == "" && to == RevisionStateActive {
		return nil
	}
	if err := from.Validate(); err != nil {
		return err
	}
	if from == RevisionStateActive && (to == RevisionStateSuperseded || to == RevisionStateInvalidated) {
		return nil
	}
	if from == RevisionStateSuperseded && to == RevisionStateInvalidated {
		return nil
	}
	return invalidDerived("revision state transition is not allowed")
}

// ValidateSourceStateTransition verifies an append-only raw source lifecycle.
func ValidateSourceStateTransition(from, to SourceState) error {
	if to.Validate() != nil {
		return invalidDerived("unsupported target source state")
	}
	if from == "" && to == SourceStateActive {
		return nil
	}
	if err := from.Validate(); err != nil {
		return err
	}
	if from == SourceStateActive && to == SourceStateForgotten {
		return nil
	}
	return invalidDerived("source state transition is not allowed")
}
