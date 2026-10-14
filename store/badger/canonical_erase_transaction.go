package badger

import (
	"context"

	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) eraseUpdate(ctx context.Context, fn func(*badger.Txn) error) error {
	return s.eraseTransaction(ctx, true, fn)
}
func (s *CanonicalStore) eraseView(ctx context.Context, fn func(*badger.Txn) error) error {
	return s.eraseTransaction(ctx, false, fn)
}
func (s *CanonicalStore) eraseTransaction(ctx context.Context, write bool, fn func(*badger.Txn) error) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	if s == nil {
		return eraseStoreFailure("canonical badger store is closed", nil)
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return eraseStoreFailure("canonical badger store is closed", nil)
	}

	if s.restoreBlocked.Load() {
		return restoreUnavailable()
	}

	guarded := func(txn *badger.Txn) error {
		if err := fn(txn); err != nil {
			return err
		}

		return canonicalContextError(ctx)
	}

	var err error
	if write {
		err = s.db.Update(guarded)
	} else {
		err = s.db.View(guarded)
	}

	if err != nil {
		return canonicalStoreError("scope erase transaction", err)
	}

	return nil
}
