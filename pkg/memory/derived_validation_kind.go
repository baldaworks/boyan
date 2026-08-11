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

// Validate verifies a supported atom candidate relationship.
func (r CandidateRelation) Validate() error {
	switch r {
	case CandidateRelationNew, CandidateRelationCoexist, CandidateRelationSupersede:
		return nil
	default:
		return invalidDerived("unsupported candidate relation")
	}
}
