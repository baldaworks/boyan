package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) collectForgetPayloadRefs(
	scope memory.Scope,
	deniedSources map[string]struct{},
	deniedRevisions map[string]struct{},
) ([]memory.PayloadRef, bool, error) {
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return nil, false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	collector := newScrubPayloadCollector(len(deniedSources) + len(deniedRevisions))
	if err := s.db.View(func(txn *badger.Txn) error {
		if err := collectForgetSourcePayloads(txn, scope, deniedSources, collector); err != nil {
			return err
		}
		if err := collectForgetRevisionPayloads(txn, scope, deniedRevisions, collector); err != nil {
			return err
		}
		return collectForgetMessagePayloads(txn, scope, deniedSources, collector)
	}); err != nil {
		return nil, false, canonicalStoreError("scan canonical forget payloads", err)
	}
	return collector.refs, collector.overflow, nil
}
