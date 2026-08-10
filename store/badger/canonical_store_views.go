package badger

import (
	"context"
	"errors"

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

func (s *CanonicalStore) LoadCanonicalRevisions(ctx context.Context, request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	revisions := make([]memory.MemoryRevision, 0, len(request.RevisionIDs))
	err := s.db.View(func(txn *badger.Txn) error {
		for _, revisionID := range request.RevisionIDs {
			key, err := canonicalKey(request.Scope, badgerRecordRevision, revisionID)
			if err != nil {
				return err
			}
			var revision memory.MemoryRevision
			if err := getCanonicalRecord(txn, key, badgerRecordRevision, &revision); err != nil {
				if errors.Is(err, badger.ErrKeyNotFound) {
					return memory.PermanentError(memory.CodeNotFound, "canonical revision is missing", nil)
				}
				return err
			}
			if err := revision.Validate(); err != nil {
				return err
			}
			revisions = append(revisions, revision)
		}
		return nil
	})
	if err != nil {
		return nil, canonicalStoreError("load canonical revisions", err)
	}
	return revisions, nil
}

func (s *CanonicalStore) ScanActiveHeads(ctx context.Context, request memory.ActiveHeadScanRequest) ([]memory.ItemHead, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	prefix, err := canonicalPrefix(request.Scope, badgerRecordHead)
	if err != nil {
		return nil, err
	}
	heads := make([]memory.ItemHead, 0, request.Limit)
	err = s.db.View(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix
		iterator := txn.NewIterator(options)
		defer iterator.Close()
		start := prefix
		if request.AfterItemID != "" {
			start, err = canonicalKey(request.Scope, badgerRecordHead, request.AfterItemID)
			if err != nil {
				return err
			}
		}
		for iterator.Seek(start); iterator.ValidForPrefix(prefix) && uint32(len(heads)) < request.Limit; iterator.Next() {
			var head memory.ItemHead
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordHead, &head); err != nil {
				return err
			}
			if request.AfterItemID != "" && head.ItemID <= request.AfterItemID {
				continue
			}
			if err := head.Validate(); err != nil {
				return err
			}
			heads = append(heads, head)
		}
		return nil
	})
	if err != nil {
		return nil, canonicalStoreError("scan canonical active heads", err)
	}
	return heads, nil
}

// ScanActiveMemory returns a bounded reconciler view without scanning historic
// revisions. Scope-level CAS on the subsequent mutation handles concurrent
// changes after this read.
func (s *CanonicalStore) ScanActiveMemory(ctx context.Context, request memory.ActiveMemoryScanRequest) ([]memory.ActiveCanonicalMemory, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	heads, err := s.ScanActiveHeads(ctx, memory.ActiveHeadScanRequest(request))
	if err != nil {
		return nil, err
	}
	active := make([]memory.ActiveCanonicalMemory, 0, len(heads))
	err = s.db.View(func(txn *badger.Txn) error {
		for _, head := range heads {
			itemKey, keyErr := canonicalKey(request.Scope, badgerRecordItem, head.ItemID)
			if keyErr != nil {
				return keyErr
			}
			revisionKey, keyErr := canonicalKey(request.Scope, badgerRecordRevision, head.RevisionID)
			if keyErr != nil {
				return keyErr
			}
			var item memory.MemoryItem
			if err := getCanonicalRecord(txn, itemKey, badgerRecordItem, &item); err != nil {
				return err
			}
			var revision memory.MemoryRevision
			if err := getCanonicalRecord(txn, revisionKey, badgerRecordRevision, &revision); err != nil {
				return err
			}
			if err := item.Validate(); err != nil {
				return memory.PermanentError(memory.CodeStoreFailure, "stored active canonical item is invalid", err)
			}
			if item.Scope != request.Scope || revision.ItemID != item.ItemID {
				return memory.PermanentError(memory.CodeStoreFailure, "stored active canonical memory is invalid", nil)
			}
			active = append(active, memory.ActiveCanonicalMemory{
				Item:       item,
				RevisionID: revision.RevisionID,
				Revision:   revision.Revision,
				Evidence:   append([]memory.EvidenceRef(nil), revision.Evidence...),
			})
		}
		return nil
	})
	if err != nil {
		return nil, canonicalStoreError("scan active canonical memory", err)
	}
	return active, nil
}
