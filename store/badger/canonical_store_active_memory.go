package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ScanActiveMemory returns a bounded reconciler view without scanning historic
// revisions. Scope-level CAS on the subsequent mutation handles concurrent
// changes after this read.
func (s *CanonicalStore) ScanActiveMemory(
	ctx context.Context,
	request memory.ActiveMemoryScanRequest,
) ([]memory.ActiveCanonicalMemory, error) {
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
		return loadActiveMemory(txn, request.Scope, heads, &active)
	})
	if err != nil {
		return nil, canonicalStoreError("scan active canonical memory", err)
	}

	return active, nil
}

func loadActiveMemory(
	txn *badger.Txn,
	scope memory.Scope,
	heads []memory.ItemHead,
	active *[]memory.ActiveCanonicalMemory,
) error {
	for _, head := range heads {
		itemKey, keyErr := canonicalKey(scope, badgerRecordItem, head.ItemID)
		if keyErr != nil {
			return keyErr
		}

		revisionKey, keyErr := canonicalKey(scope, badgerRecordRevision, head.RevisionID)
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

		if item.Scope != scope || revision.ItemID != item.ItemID {
			return memory.PermanentError(memory.CodeStoreFailure, "stored active canonical memory is invalid", nil)
		}

		*active = append(*active, memory.ActiveCanonicalMemory{
			Item:       item,
			RevisionID: revision.RevisionID,
			Revision:   revision.Revision,
			Evidence:   append([]memory.EvidenceRef(nil), revision.Evidence...),
		})
	}

	return nil
}
