package memory

func invalidDerived(message string) error {
	return PermanentError(CodeInvalidDerived, message, nil)
}

func limitExceeded(message string) error {
	return PermanentError(CodeLimitExceeded, message, nil)
}

func validateUniqueCanonicalIDs(ids []string, name string) error {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !isCanonicalID(id) {
			return invalidDerived(name + " identity is invalid")
		}
		if _, exists := seen[id]; exists {
			return invalidDerived(name + " identity is duplicated")
		}
		seen[id] = struct{}{}
	}
	return nil
}
