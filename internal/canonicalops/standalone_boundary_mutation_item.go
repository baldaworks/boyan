package canonicalops

import memory "github.com/baldaworks/boyan/pkg/memory"

func existingBoundaryItems(view memory.CanonicalBoundaryView) map[string]struct{} {
	existingItems := make(map[string]struct{}, len(view.Snapshot.Scenarios)+len(view.Snapshot.Profiles))
	for _, scenario := range view.Snapshot.Scenarios {
		existingItems[scenario.Meta.ItemID] = struct{}{}
	}

	for _, profile := range view.Snapshot.Profiles {
		existingItems[profile.Meta.ItemID] = struct{}{}
	}

	return existingItems
}

func ensureBoundaryItem(
	existingItems map[string]struct{},
	items *[]memory.MemoryItem,
	scope memory.Scope,
	itemID string,
) {
	if _, exists := existingItems[itemID]; exists {
		return
	}

	*items = append(*items, memory.MemoryItem{
		ItemID:    itemID,
		Scope:     scope,
		Kind:      memory.MemoryKindState,
		MemoryKey: derivedMemoryKey(itemID),
	})
	existingItems[itemID] = struct{}{}
}
