package memory

// Validate verifies a supported derived-memory kind.
func (k DerivedKind) Validate() error {
	switch k {
	case DerivedKindAtom, DerivedKindScenario, DerivedKindProfile:
		return nil
	default:
		return invalidDerived("unsupported derived kind")
	}
}

// Validate verifies a supported atom category.
func (c AtomCategory) Validate() error {
	switch c {
	case AtomCategoryFact, AtomCategoryPreference, AtomCategoryConstraint, AtomCategoryDecision, AtomCategoryEvent:
		return nil
	default:
		return invalidDerived("unsupported atom category")
	}
}

// Validate verifies a supported derived revision state.
func (s RevisionState) Validate() error {
	switch s {
	case RevisionStateActive, RevisionStateSuperseded, RevisionStateInvalidated:
		return nil
	default:
		return invalidDerived("unsupported revision state")
	}
}

// Validate verifies a supported raw source state.
func (s SourceState) Validate() error {
	switch s {
	case SourceStateActive, SourceStateForgotten:
		return nil
	default:
		return invalidDerived("unsupported source state")
	}
}

// Validate verifies a supported atom candidate relationship.
func (r CandidateRelation) Validate() error {
	switch r {
	case CandidateRelationNew, CandidateRelationCoexist, CandidateRelationSupersede:
		return nil
	default:
		return invalidDerived("unsupported candidate relation")
	}
}

// Validate verifies a supported idempotent processing stage.
func (s OperationStage) Validate() error {
	switch s {
	case OperationStageAtoms, OperationStageScenarios, OperationStageProfile:
		return nil
	default:
		return invalidDerived("unsupported operation stage")
	}
}

// Validate verifies a supported profile synthesis disposition.
func (d ProfileDisposition) Validate() error {
	switch d {
	case ProfileDispositionSkip, ProfileDispositionUpsert:
		return nil
	default:
		return invalidDerived("unsupported profile candidate disposition")
	}
}

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
