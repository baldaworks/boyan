package memory

// Reconcile validates one exact-scope candidate against a bounded active view.
func (p PolicyRegistry) Reconcile(
	scope Scope,
	candidate SemanticCandidate,
	active []MemoryItem,
) (
	Reconciliation,
	error,
) {
	view := make([]ActiveMemoryItem, len(active))
	for index, item := range active {
		view[index].Item = item
	}

	return p.ReconcileActive(scope, candidate, view)
}

// ReconcileActive validates one exact-scope candidate against bounded canonical context.
func (p PolicyRegistry) ReconcileActive(
	scope Scope,
	candidate SemanticCandidate,
	active []ActiveMemoryItem,
) (
	Reconciliation,
	error,
) {
	if err := scope.Validate(); err != nil {
		return Reconciliation{}, err
	}

	if err := validateReconciliationInputs(p, candidate, active); err != nil {
		return Reconciliation{}, err
	}

	matcher, err := p.PrepareActiveMemoryMatcher(scope, candidate)
	if err != nil {
		return Reconciliation{}, err
	}

	item := matcher.item

	match, err := matchReconciliationItem(matcher, &item, active)
	if err != nil {
		return Reconciliation{}, err
	}

	reconciliation, err := buildReconciliation(candidate, item, match)
	if err != nil {
		return Reconciliation{}, err
	}

	return reconciliation, nil
}

func validateReconciliationInputs(p PolicyRegistry, candidate SemanticCandidate, active []ActiveMemoryItem) error {
	if !isCanonicalID(p.Version) {
		return invalidDerived("reconciliation policy version is required")
	}

	if err := candidate.Memory.Validate(); err != nil {
		return err
	}

	if candidate.Kind != candidate.Memory.Kind {
		return invalidDerived("semantic candidate kind does not match memory candidate")
	}

	if len(active) > MaxSnapshotItems {
		return limitExceeded("reconciliation active view exceeds the limit")
	}

	return nil
}
