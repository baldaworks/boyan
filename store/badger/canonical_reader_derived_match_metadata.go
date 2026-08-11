package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func derivedRecordLegacyKind(record memory.RecallRecord) memory.DerivedKind {
	legacyKind := memory.DerivedKindAtom
	if record.LegacyKind != nil {
		legacyKind = *record.LegacyKind
	}
	return legacyKind
}

func derivedRecordCategory(record memory.RecallRecord) memory.AtomCategory {
	category := recallCategory(record.Kind)
	if record.Category != nil {
		category = *record.Category
	}
	return category
}
