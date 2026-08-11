package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ScanScopeChanges returns a bounded ordered tail for projection recovery.
func (s *CanonicalStore) ScanScopeChanges(ctx context.Context, scope memory.Scope, after uint64, limit uint32) ([]memory.ScopeChange, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if limit == 0 || limit > 512 {
		return nil, memory.PermanentError(memory.CodeLimitExceeded, "canonical change scan limit is invalid", nil)
	}
	prefix, err := badgerScopeChangePrefix(scope)
	if err != nil {
		return nil, err
	}
	changes := make([]memory.ScopeChange, 0, limit)
	err = s.db.View(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix
		iterator := txn.NewIterator(options)
		defer iterator.Close()
		start, err := badgerScopeChangeKey(scope, after)
		if err != nil {
			return err
		}
		for iterator.Seek(start); iterator.ValidForPrefix(prefix) && uint32(len(changes)) < limit; iterator.Next() {
			var change memory.ScopeChange
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordChange, &change); err != nil {
				return err
			}
			if err := change.Validate(); err != nil {
				return err
			}
			if change.Sequence <= after {
				continue
			}
			changes = append(changes, change)
		}
		return nil
	})
	if err != nil {
		return nil, canonicalStoreError("scan canonical scope changes", err)
	}
	return changes, nil
}
