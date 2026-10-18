package canonicalsql

import memory "github.com/baldaworks/boyan/pkg/memory"

func matchDerivedSearchRecord(
	record memory.RecallRecord,
	normalized memory.DerivedSearchRequest,
) (memory.DerivedKind, memory.AtomCategory, bool, error) {
	matches, err := recordMatchesDerivedSearchFilters(record, normalized)
	if err != nil || !matches {
		return "", "", matches, err
	}

	legacyKind := derivedRecordLegacyKind(record)
	if normalized.Kind != nil && *normalized.Kind != legacyKind {
		return "", "", false, nil
	}

	category := derivedRecordCategory(record)
	if normalized.Category != nil &&
		(record.Category == nil || *normalized.Category != category) {
		return "", "", false, nil
	}

	return legacyKind, category, true, nil
}
