package memory

// Validate verifies a complete scenario revision and its deterministic identities.
func (s Scenario) Validate() error {
	topicKey, err := validateDerivedKey("scenario topic key", s.TopicKey)
	if err != nil {
		return err
	}

	if topicKey != s.TopicKey {
		return invalidDerived("scenario topic key must be normalized")
	}

	title, err := validateDerivedText("scenario title", s.Title)
	if err != nil {
		return err
	}

	if title != s.Title {
		return invalidDerived("scenario title must be normalized")
	}

	summary, err := validateDerivedText("scenario summary", s.Summary)
	if err != nil {
		return err
	}

	if summary != s.Summary {
		return invalidDerived("scenario summary must be normalized")
	}

	itemID, err := ScenarioItemID(s.Meta.Scope, s.TopicKey)
	if err != nil {
		return err
	}

	return validateRevisionMeta(s.Meta, DerivedKindScenario, itemID, []string{s.TopicKey, s.Title, s.Summary}, false)
}
