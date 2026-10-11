package memory

func normalizeSemanticCandidate(candidate SemanticCandidate) (string, string, []string, error) {
	subject, err := canonicalSemanticPart(candidate.Subject)
	if err != nil {
		return "", "", nil, err
	}

	predicate, err := canonicalSemanticPart(candidate.Predicate)
	if err != nil {
		return "", "", nil, err
	}

	qualifiers := make([]string, len(candidate.Qualifiers))
	for index, qualifier := range candidate.Qualifiers {
		qualifiers[index], err = canonicalSemanticPart(qualifier)
		if err != nil {
			return "", "", nil, err
		}
	}

	return subject, predicate, qualifiers, nil
}
