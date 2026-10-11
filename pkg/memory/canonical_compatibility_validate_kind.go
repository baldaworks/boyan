package memory

func (p CanonicalCompatibilityPayload) validateCompatibilityKindFields() error {
	switch p.Kind {
	case DerivedKindAtom:
		if p.Category == nil {
			return invalidDerived("canonical atom compatibility category is required")
		}

		return p.Category.Validate()
	case DerivedKindScenario:
		return p.validateScenarioCompatibility()
	case DerivedKindProfile:
		if p.Category != nil || p.TopicKey != "" || p.Title != "" {
			return invalidDerived("canonical profile compatibility fields are invalid")
		}
	}

	return nil
}

func (p CanonicalCompatibilityPayload) validateScenarioCompatibility() error {
	if p.Category != nil || p.TopicKey == "" || p.Title == "" {
		return invalidDerived("canonical scenario compatibility fields are invalid")
	}

	return nil
}
