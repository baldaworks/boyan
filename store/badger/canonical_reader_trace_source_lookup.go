package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (r *CanonicalReader) loadCanonicalTraceSource(
	scope memory.Scope,
	sourceID string,
) (memory.CanonicalSourceRecord, error) {
	var source memory.CanonicalSourceRecord

	key, err := canonicalKey(scope, badgerRecordSource, sourceID)
	if err != nil {
		return memory.CanonicalSourceRecord{}, err
	}

	if err := r.store.db.View(func(txn *badger.Txn) error {
		if err := checkDeniedSource(txn, scope, sourceID); err != nil {
			return err
		}

		return getCanonicalRecord(txn, key, badgerRecordSource, &source)
	}); err != nil {
		return memory.CanonicalSourceRecord{}, canonicalStoreError("load canonical trace source", err)
	}

	if err := source.Validate(); err != nil {
		return memory.CanonicalSourceRecord{}, err
	}

	if source.Sensitivity != memory.SensitivityStandard {
		return memory.CanonicalSourceRecord{}, memory.PermanentError(memory.CodeForgotten, "trace source is unavailable", nil)
	}

	return source, nil
}
