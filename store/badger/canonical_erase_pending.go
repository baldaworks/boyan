package badger

import (
	"bytes"
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ListPendingScopeErases enumerates only pending fences in physical-key order.
// The opaque exclusive cursor is valid across restart; completed/rejected work
// is excluded. A page never exceeds the canonical read bound.
func (s *CanonicalStore) ListPendingScopeErases(
	ctx context.Context,
	cursor []byte,
	limit int,
) ([]memory.ScopeEraseJournal, []byte, error) {
	if limit < 1 || limit > memory.MaxCanonicalReadRecords {
		return nil, nil, memory.PermanentError(memory.CodeLimitExceeded, "pending erase page limit is invalid", nil)
	}

	prefix, err := eraseGlobalPrefix(badgerRecordEraseFence)
	if err != nil {
		return nil, nil, err
	}

	if len(cursor) > 0 && !bytes.HasPrefix(cursor, prefix) {
		return nil, nil, eraseStoreFailure("pending erase cursor is invalid", nil)
	}

	var (
		journals []memory.ScopeEraseJournal
		next     []byte
	)

	err = s.eraseView(ctx, func(txn *badger.Txn) error {
		var err error

		journals, next, err = listPendingEraseTxn(txn, prefix, cursor, limit)

		return err
	})

	return journals, next, err
}

func pendingEraseAtKey(txn *badger.Txn, key []byte) (memory.ScopeEraseJournal, error) {
	parts, _, err := decodeCanonicalLogicalKey(key)
	if err != nil {
		return memory.ScopeEraseJournal{}, err
	}

	scope := memory.Scope{Key: parts[1], Kind: memory.ScopeKind(parts[2])}

	var fence scopeEraseFence
	if err := getCanonicalRecord(txn, key, badgerRecordEraseFence, &fence); err != nil {
		return memory.ScopeEraseJournal{}, err
	}

	journal, found, err := loadScopeEraseTxn(txn, scope, fence.OperationID)
	if err != nil {
		return journal, err
	}

	if !found || eraseTerminal(journal.Stage) {
		return journal, eraseStoreFailure("pending erase journal is absent or terminal", nil)
	}

	return journal, validateScopeEraseOwnership(txn, journal)
}

func listPendingEraseTxn(
	txn *badger.Txn,
	prefix,
	cursor []byte,
	limit int,
) ([]memory.ScopeEraseJournal, []byte, error) {
	var (
		journals []memory.ScopeEraseJournal
		next     []byte
	)

	options := badger.DefaultIteratorOptions
	options.PrefetchValues = false

	iterator := txn.NewIterator(options)
	defer iterator.Close()

	seek := prefix
	if len(cursor) > 0 {
		seek = cursor
	}

	iterator.Seek(seek)

	if iterator.ValidForPrefix(prefix) && bytes.Equal(iterator.Item().Key(), cursor) {
		iterator.Next()
	}

	for iterator.ValidForPrefix(prefix) && len(journals) < limit {
		key := iterator.Item().KeyCopy(nil)

		journal, err := pendingEraseAtKey(txn, key)
		if err != nil {
			return nil, nil, err
		}

		journals = append(journals, journal)
		next = key

		iterator.Next()
	}

	if !iterator.ValidForPrefix(prefix) {
		next = nil
	}

	return journals, next, nil
}
