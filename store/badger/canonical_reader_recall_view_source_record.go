package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadCanonicalSourceRecord(
	txn *badger.Txn,
	scope memory.Scope,
	sourceID string,
) (memory.CanonicalSourceRecord, error) {
	sourceKey, err := canonicalKey(scope, badgerRecordSource, sourceID)
	if err != nil {
		return memory.CanonicalSourceRecord{}, err
	}

	var source memory.CanonicalSourceRecord
	if err := getPrivacyPreparationRecord(txn, sourceKey, badgerRecordSource, &source); err != nil {
		return memory.CanonicalSourceRecord{}, err
	}

	if err := source.Validate(); err != nil || source.Scope != scope || source.SourceID != sourceID {
		return memory.CanonicalSourceRecord{}, memory.PermanentError(
			memory.CodeStoreFailure,
			"stored canonical source is invalid",
			err,
		)
	}

	if source.Sensitivity != memory.SensitivityStandard {
		return memory.CanonicalSourceRecord{}, errCanonicalDenied
	}

	return source, nil
}
