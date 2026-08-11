package memory

// DerivedRevisionID derives an immutable revision identity from content and provenance.
// Provenance order does not affect the result.
func DerivedRevisionID(
	scope Scope,
	itemID string,
	operationID string,
	contentParts []string,
	provenance Provenance,
	supersedes *RevisionRef,
) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", err
	}
	if !isCanonicalID(itemID) {
		return "", invalidDerived("derived item id is required")
	}
	if !isCanonicalID(operationID) {
		return "", invalidDerived("derived operation id is required")
	}
	if len(contentParts) == 0 {
		return "", invalidDerived("derived revision content is required")
	}

	normalizedParts := make([]string, len(contentParts))
	for index, part := range contentParts {
		normalized, err := validateDerivedText("derived revision content", part)
		if err != nil {
			return "", err
		}
		normalizedParts[index] = normalized
	}
	if err := provenance.Validate(scope); err != nil {
		return "", err
	}
	if supersedes != nil {
		if err := supersedes.Validate(); err != nil {
			return "", err
		}
	}

	parts := []string{scope.Key, string(scope.Kind), itemID, operationID}
	parts = append(parts, normalizedParts...)
	parts = append(parts, canonicalProvenanceParts(provenance)...)
	if supersedes != nil {
		parts = append(parts, "supersedes", supersedes.ItemID, supersedes.RevisionID)
	}
	return derivedStableID("revision", parts...), nil
}
