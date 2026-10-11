package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// WalkProjectionManifests visits disposable generation metadata and its durable
// active reference in one read transaction. Callbacks must not mutate the store.
func (s *CanonicalStore) WalkProjectionManifests(
	ctx context.Context,
	projectionID string,
	visit func(memory.ProjectionManifest, bool) error,
) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	prefix, err := memory.TupleKey(canonicalNamespace, canonicalVersion, badgerRecordProjectionManifest)
	if err != nil {
		return err
	}

	return s.db.View(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix

		iterator := txn.NewIterator(options)
		defer iterator.Close()

		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}

			var manifest memory.ProjectionManifest
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordProjectionManifest, &manifest); err != nil {
				return err
			}

			if err := manifest.Validate(); err != nil {
				return err
			}

			if manifest.ProjectionID != projectionID {
				continue
			}

			active, err := activeProjectionGeneration(txn, manifest.Scope, projectionID)
			if err != nil {
				return err
			}

			if err := visit(manifest, active == manifest.GenerationID); err != nil {
				return err
			}
		}

		return nil
	})
}

func activeProjectionGeneration(txn *badger.Txn, scope memory.Scope, projectionID string) (string, error) {
	key, err := canonicalKey(scope, badgerRecordProjectionActive, projectionID)
	if err != nil {
		return "", err
	}

	var active badgerProjectionActive
	if err := getCanonicalRecord(txn, key, badgerRecordProjectionActive, &active); err != nil &&
		!errors.Is(err, badger.ErrKeyNotFound) {
		return "", err
	}

	return active.GenerationID, nil
}
