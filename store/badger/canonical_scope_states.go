package badger

import (
	"bytes"
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ListCanonicalScopeStates returns a bounded page in durable physical-key order.
func (s *CanonicalStore) ListCanonicalScopeStates(
	ctx context.Context, cursor []byte, limit int,
) ([]memory.ScopeState, []byte, error) {
	if limit < 1 || limit > memory.MaxCanonicalReadRecords {
		return nil, nil, memory.PermanentError(
			memory.CodeLimitExceeded, "scope state page limit is invalid", nil,
		)
	}

	prefix, err := eraseGlobalPrefix(badgerRecordScope)
	if err != nil {
		return nil, nil, err
	}

	if len(cursor) > 0 {
		if !bytes.HasPrefix(cursor, prefix) {
			return nil, nil, eraseStoreFailure("scope state cursor is invalid", nil)
		}

		if _, _, err := decodeCanonicalLogicalKey(cursor); err != nil {
			return nil, nil, err
		}
	}

	var (
		states []memory.ScopeState
		next   []byte
	)

	err = s.eraseView(ctx, func(txn *badger.Txn) error {
		var err error

		states, next, err = listScopeStatesTxn(ctx, txn, prefix, cursor, limit)

		return err
	})
	if err != nil {
		return nil, nil, err
	}

	return states, next, nil
}

func listScopeStatesTxn(
	ctx context.Context, txn *badger.Txn, prefix, cursor []byte, limit int,
) ([]memory.ScopeState, []byte, error) {
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

	var (
		states []memory.ScopeState
		next   []byte
	)

	for iterator.ValidForPrefix(prefix) && len(states) < limit {
		if err := canonicalContextError(ctx); err != nil {
			return nil, nil, err
		}

		key := iterator.Item().KeyCopy(nil)

		state, err := scopeStateAtKey(txn, key)
		if err != nil {
			return nil, nil, err
		}

		states = append(states, state)
		next = key

		iterator.Next()
	}

	if !iterator.ValidForPrefix(prefix) {
		next = nil
	}

	return states, next, nil
}

func scopeStateAtKey(txn *badger.Txn, key []byte) (memory.ScopeState, error) {
	parts, _, err := decodeCanonicalLogicalKey(key)
	if err != nil {
		return memory.ScopeState{}, err
	}

	var state memory.ScopeState
	if err := getCanonicalRecord(txn, key, badgerRecordScope, &state); err != nil {
		return state, err
	}

	if err := state.Validate(); err != nil {
		return state, eraseStoreFailure("stored scope state is invalid", err)
	}

	if state.Scope.Key != parts[1] || string(state.Scope.Kind) != parts[2] {
		return state, eraseStoreFailure("stored scope state identity is invalid", nil)
	}

	return state, nil
}

var _ memory.CanonicalScopeStatePager = (*CanonicalStore)(nil)
