package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func appendDerivedSearchResults(records []memory.RecallRecord, normalized memory.DerivedSearchRequest) ([]memory.DerivedReference, error) {
	results := make([]memory.DerivedReference, 0, len(records))
	for _, record := range records {
		legacyKind, category, ok, err := matchDerivedSearchRecord(record, normalized)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		ref, ok := canonicalDerivedReference(record, legacyKind, category)
		if !ok {
			continue
		}
		results = append(results, ref)
	}
	return results, nil
}
