package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func listCanonicalSourceRefsPage(
	txn *badger.Txn,
	scope memory.Scope,
	prefix []byte,
	afterSourceID string,
	limit uint32,
) ([]memory.SourceRef, string, error) {
	refs := make([]memory.SourceRef, 0, limit)
	next := ""

	iterator := txn.NewIterator(badger.DefaultIteratorOptions)
	defer iterator.Close()

	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix) && uint32(len(refs)) < limit; iterator.Next() {
		var source memory.CanonicalSourceRecord
		if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordSource, &source); err != nil {
			return nil, "", err
		}

		if source.SourceID <= afterSourceID {
			continue
		}

		ref, include, err := listedCanonicalSourceRef(txn, scope, source)
		if err != nil {
			return nil, "", err
		}

		if !include {
			continue
		}

		refs = append(refs, ref)
		next = source.SourceID
	}

	return refs, next, nil
}
