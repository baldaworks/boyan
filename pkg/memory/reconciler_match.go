package memory

func matchReconciliationItem(
	scope Scope,
	candidate SemanticCandidate,
	item *MemoryItem,
	active []ActiveMemoryItem,
) (*MemoryItem, error) {
	var match *MemoryItem
	for _, existing := range active {
		if err := existing.Item.Validate(); err != nil || existing.Item.Scope != scope {
			return nil, PermanentError(CodeScopeViolation, "reconciliation active item is invalid or foreign", nil)
		}
		if candidate.Kind == MemoryKindState && existing.Item.MemoryKey == item.MemoryKey {
			if match != nil {
				return nil, invalidDerived("reconciliation has ambiguous active state matches")
			}
			matched := existing.Item
			match = &matched
		}
		if candidate.Kind == MemoryKindEvent && sameEventEvidence(existing.Evidence, candidate.Memory.Evidence) {
			if match != nil {
				return nil, invalidDerived("reconciliation has ambiguous active event matches")
			}
			item.ItemID = existing.Item.ItemID
			matched := existing.Item
			match = &matched
		}
	}
	return match, nil
}
