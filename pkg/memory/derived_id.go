package memory

// AtomItemID derives the stable logical identity of one normalized atom.
func AtomItemID(scope Scope, category AtomCategory, text string) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	if err := category.Validate(); err != nil {
		return "", err
	}
	normalized, err := validateDerivedText("atom text", text)
	if err != nil {
		return "", err
	}
	return derivedStableID("atom", scope.Key, string(scope.Kind), string(category), normalized), nil
}

// ScenarioItemID derives the stable logical identity of one scope topic.
func ScenarioItemID(scope Scope, topicKey string) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	normalized, err := validateDerivedKey("scenario topic key", topicKey)
	if err != nil {
		return "", err
	}
	return derivedStableID("scenario", scope.Key, string(scope.Kind), normalized), nil
}

// ProfileItemID derives the single logical profile identity for one exact scope.
func ProfileItemID(scope Scope) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	return derivedStableID("profile", scope.Key, string(scope.Kind)), nil
}
