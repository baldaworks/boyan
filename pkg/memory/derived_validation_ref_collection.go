package memory

func containsRevisionRef(refs []RevisionRef, want RevisionRef) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}

	return false
}

func validateRevisionRefs(refs []RevisionRef) error {
	if len(refs) > MaxSourcesPerRevision {
		return limitExceeded("derived parent references exceed the limit")
	}

	seen := make(map[RevisionRef]struct{}, len(refs))
	for _, ref := range refs {
		if err := ref.Validate(); err != nil {
			return err
		}

		if _, ok := seen[ref]; ok {
			return invalidDerived("duplicate derived revision reference")
		}

		seen[ref] = struct{}{}
	}

	return nil
}
