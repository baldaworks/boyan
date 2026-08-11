package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

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
