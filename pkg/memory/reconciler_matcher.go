package memory

// ActiveMemoryMatcher holds one validated candidate's normalized policy identity.
// It can scan arbitrarily many bounded pages while retaining only its matches.
type ActiveMemoryMatcher struct {
	scope    Scope
	item     MemoryItem
	kind     MemoryKind
	evidence []EvidenceRef
}

// PrepareActiveMemoryMatcher applies the same normalization and policy as ReconcileActive.
func (p PolicyRegistry) PrepareActiveMemoryMatcher(scope Scope,
	candidate SemanticCandidate) (ActiveMemoryMatcher,
	error) {
	if err := scope.Validate(); err != nil {
		return ActiveMemoryMatcher{}, err
	}

	if err := validateReconciliationInputs(p, candidate, nil); err != nil {
		return ActiveMemoryMatcher{}, err
	}

	subject, predicate, qualifiers, err := normalizeSemanticCandidate(candidate)
	if err != nil {
		return ActiveMemoryMatcher{}, err
	}

	item, err := buildReconciliationItem(p, scope, candidate, subject, predicate, qualifiers)
	if err != nil {
		return ActiveMemoryMatcher{}, err
	}

	if err := item.Validate(); err != nil {
		return ActiveMemoryMatcher{}, err
	}

	return ActiveMemoryMatcher{scope: scope,
			item: item,
			kind: candidate.Kind,
			evidence: append([]EvidenceRef(nil),
				candidate.Memory.Evidence...)},
		nil
}

// Matches validates exact-scope metadata before applying state-key or event-evidence equality.
func (m ActiveMemoryMatcher) Matches(active ActiveMemoryItem) (bool, error) {
	if err := active.Item.Validate(); err != nil || active.Item.Scope != m.scope {
		return false,
			PermanentError(CodeScopeViolation,
				"reconciliation active item is invalid or foreign",
				nil)
	}

	if m.kind == MemoryKindState {
		return active.Item.MemoryKey == m.item.MemoryKey, nil
	}

	return sameEventEvidence(active.Evidence, m.evidence), nil
}
