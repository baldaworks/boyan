package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// SourceRevisionBatch returns a bounded, resumable source-provenance batch.
// The next cursor is the final revision ID returned; an empty cursor means
// the source has no further indexed descendants in this batch direction.
func (s *CanonicalStore) SourceRevisionBatch(
	ctx context.Context,
	scope memory.Scope,
	sourceID, afterRevisionID string,
	limit uint32,
) ([]string, string, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, "", err
	}

	if err := scope.Validate(); err != nil {
		return nil, "", err
	}

	if err := validateSourceRevisionBatch(sourceID, limit); err != nil {
		return nil, "", err
	}

	if s == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	prefix, err := badgerProvenancePrefix(scope, badgerRecordSourceRevision, sourceID)
	if err != nil {
		return nil, "", err
	}

	results := make([]string, 0, limit)

	err = s.db.View(func(txn *badger.Txn) error {
		return scanSourceRevisionBatch(txn, prefix, afterRevisionID, limit, &results)
	})
	if err != nil {
		return nil, "", canonicalStoreError("scan source provenance", err)
	}

	if len(results) == 0 {
		return results, "", nil
	}

	return results, results[len(results)-1], nil
}

func scanSourceRevisionBatch(
	txn *badger.Txn,
	prefix []byte,
	afterRevisionID string,
	limit uint32,
	results *[]string,
) error {
	options := badger.DefaultIteratorOptions
	options.Prefix = prefix

	iterator := txn.NewIterator(options)
	defer iterator.Close()

	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix) && uint32(len(*results)) < limit; iterator.Next() {
		var edge badgerProvenanceEdge
		if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordSourceRevision, &edge); err != nil {
			return err
		}

		if edge.ChildRevisionID > afterRevisionID {
			*results = append(*results, edge.ChildRevisionID)
		}
	}

	return nil
}

func validateSourceRevisionBatch(sourceID string, limit uint32) error {
	if sourceID == "" || limit == 0 || limit > 512 {
		return memory.PermanentError(memory.CodeLimitExceeded, "source provenance batch limit is invalid", nil)
	}

	return nil
}
