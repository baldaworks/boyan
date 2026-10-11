package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func recallCategory(kind memory.MemoryKind) memory.AtomCategory {
	if kind == memory.MemoryKindEvent {
		return memory.AtomCategoryEvent
	}

	return memory.AtomCategoryFact
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}

	for _, existing := range values {
		if existing == value {
			return values
		}
	}

	return append(values, value)
}
