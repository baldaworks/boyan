package memory

func matchReconciliationItem(matcher ActiveMemoryMatcher,
	item *MemoryItem,
	active []ActiveMemoryItem) (*MemoryItem,
	error) {
	var match *MemoryItem

	for _, existing := range active {
		matched, err := matcher.Matches(existing)
		if err != nil {
			return nil, err
		}

		if !matched {
			continue
		}

		if match != nil {
			return nil, invalidDerived("reconciliation has ambiguous active matches")
		}

		copyOf := existing.Item
		match = &copyOf

		if matcher.kind == MemoryKindEvent {
			item.ItemID = existing.Item.ItemID
		}
	}

	return match, nil
}
