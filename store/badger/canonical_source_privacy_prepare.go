package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const sourcePrivacyBatchSize = 32

// PrepareSourcePrivacy reconstructs bounded source authority before old stores
// serve canonical writers or trace. A failed pass restarts from the beginning.
func (s *CanonicalStore) PrepareSourcePrivacy(ctx context.Context) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	if s == nil {
		return invalidSourcePrivacy()
	}

	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()

	if s.db == nil {
		return invalidSourcePrivacy()
	}

	if s.restoreBlocked.Load() {
		return s.resumeValidatedPrivacyRestore(ctx)
	}

	return s.prepareSourcePrivacyLocked(ctx, false)
}

// The caller owns maintenanceMu. Restore can force reconstruction without
// trusting a copied readiness marker or acquiring the maintenance lock twice.
func (s *CanonicalStore) prepareSourcePrivacyLocked(ctx context.Context, force bool) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	if !force {
		err := s.db.View(requireSourcePrivacyReady)
		if err == nil {
			return nil
		}

		if code, _, ok := memory.ClassifyError(err); !ok || code != memory.CodeUnavailable {
			return err
		}
	}

	if err := s.db.Update(func(txn *badger.Txn) error { return txn.Delete(sourcePrivacyReadinessKey()) }); err != nil {
		return err
	}

	passes := []struct {
		kind  string
		visit func(*badger.Txn, []byte, memory.Scope, string) error
	}{
		{badgerRecordSource, s.initializeHistoricalSourcePrivacy},
		{badgerRecordMessage, s.collectHistoricalPrivacyMessage},
		{badgerRecordRevision, s.collectHistoricalPrivacyRevision},
		{badgerRecordSource, s.completeHistoricalSourcePrivacy},
	}
	for _, pass := range passes {
		if err := s.scanSourcePrivacyBatches(ctx, pass.kind, pass.visit); err != nil {
			return canonicalStoreError("prepare canonical source privacy", err)
		}
	}

	return s.db.Update(func(txn *badger.Txn) error {
		if err := canonicalContextError(ctx); err != nil {
			return err
		}

		return publishSourcePrivacyReady(txn)
	})
}

func (s *CanonicalStore) scanSourcePrivacyBatches(ctx context.Context, kind string,
	visit func(*badger.Txn, []byte, memory.Scope, string) error) error {
	prefix, _ := memory.TupleKey(canonicalNamespace, canonicalVersion, kind)

	var after []byte

	for {
		if err := canonicalContextError(ctx); err != nil {
			return err
		}

		keys, err := s.sourcePrivacyBatchKeys(prefix, after)
		if err != nil || len(keys) == 0 {
			return err
		}

		for _, key := range keys {
			if err := s.applyPrivacyPreparationRecord(ctx, key, visit); err != nil {
				return err
			}
		}

		after = keys[len(keys)-1]
		if s.afterSourcePrivacyBatch != nil {
			if err := s.afterSourcePrivacyBatch(kind, len(keys)); err != nil {
				return err
			}
		}
	}
}

func (s *CanonicalStore) sourcePrivacyBatchKeys(prefix, after []byte) ([][]byte, error) {
	keys := make([][]byte, 0, sourcePrivacyBatchSize)
	err := s.db.View(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix
		options.PrefetchValues = false

		iterator := txn.NewIterator(options)
		defer iterator.Close()

		start := prefix
		if len(after) > 0 {
			start = after
		}

		for iterator.Seek(start); iterator.ValidForPrefix(prefix) && len(keys) < sourcePrivacyBatchSize; iterator.Next() {
			key := iterator.Item().KeyCopy(nil)
			if len(after) > 0 && string(key) == string(after) {
				continue
			}

			keys = append(keys, key)
		}

		return nil
	})

	return keys, err
}

func privacyMissing(err error) bool { return errors.Is(err, badger.ErrKeyNotFound) }

func (s *CanonicalStore) applyPrivacyPreparationRecord(ctx context.Context, key []byte,
	visit func(*badger.Txn, []byte, memory.Scope, string) error) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	components, _, err := decodeCanonicalLogicalKey(key)
	if err != nil || len(components) != canonicalRecordComponents {
		return invalidSourcePrivacy()
	}

	scope := memory.Scope{Key: components[1], Kind: memory.ScopeKind(components[2])}
	if err := scope.Validate(); err != nil {
		return invalidSourcePrivacy()
	}

	return s.db.Update(func(txn *badger.Txn) error {
		if err := canonicalContextError(ctx); err != nil {
			return err
		}

		if err := visit(txn, key, scope, components[3]); err != nil {
			return err
		}

		return canonicalContextError(ctx)
	})
}
