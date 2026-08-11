package memory

// Reconcile validates one exact-scope candidate against a bounded active view.
func (p PolicyRegistry) Reconcile(scope Scope, candidate SemanticCandidate, active []MemoryItem) (Reconciliation, error) {
	view := make([]ActiveMemoryItem, len(active))
	for index, item := range active {
		view[index].Item = item
	}
	return p.ReconcileActive(scope, candidate, view)
}

// ReconcileActive validates one exact-scope candidate against bounded canonical context.
func (p PolicyRegistry) ReconcileActive(scope Scope, candidate SemanticCandidate, active []ActiveMemoryItem) (Reconciliation, error) {
	if err := scope.Validate(); err != nil {
		return Reconciliation{}, err
	}
	if !isCanonicalID(p.Version) {
		return Reconciliation{}, invalidDerived("reconciliation policy version is required")
	}
	if err := candidate.Memory.Validate(); err != nil {
		return Reconciliation{}, err
	}
	if candidate.Kind != candidate.Memory.Kind {
		return Reconciliation{}, invalidDerived("semantic candidate kind does not match memory candidate")
	}
	if len(active) > MaxSnapshotItems {
		return Reconciliation{}, limitExceeded("reconciliation active view exceeds the limit")
	}
	subject, predicate, qualifiers, err := normalizeSemanticCandidate(candidate)
	if err != nil {
		return Reconciliation{}, err
	}
	item, err := buildReconciliationItem(p, scope, candidate, subject, predicate, qualifiers)
	if err := item.Validate(); err != nil {
		return Reconciliation{}, err
	}
	match, err := matchReconciliationItem(scope, candidate, &item, active)
	if err != nil {
		return Reconciliation{}, err
	}
	reconciliation, err := buildReconciliation(candidate, item, match)
	if err != nil {
		return Reconciliation{}, err
	}
	return reconciliation, nil
}
