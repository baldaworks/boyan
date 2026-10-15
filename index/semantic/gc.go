package semantic

import (
	"bytes"
	"context"
	"fmt"

	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const (
	garbageDeleteBatch           = 1000
	startupActiveValidationBatch = 1000
	hashesPerGeneration          = 2
)

// validateActiveGenerations checks every advertised generation before startup
// garbage collection can erase unreachable evidence. Pointer enumeration is
// paged and each ready generation is loaded under the existing 4096-chunk and
// 64-MiB encoded-size limits. Corruption is fail-closed: Open leaves the active
// pointer and all generation rows untouched for explicit disposable repair.
func (store *Store) validateActiveGenerations(ctx context.Context) error {
	seek := activePrefix()

	for {
		keys, next, err := store.activeKeyBatch(ctx, seek)
		if err != nil {
			return err
		}

		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return err
			}

			if err := store.validateActiveKey(ctx, key); err != nil {
				return err
			}
		}

		if next == nil {
			return nil
		}

		seek = next
	}
}

func (store *Store) activeKeyBatch(ctx context.Context, seek []byte) ([][]byte, []byte, error) {
	var (
		keys [][]byte
		next []byte
	)

	err := store.db.View(func(transaction *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = activePrefix()
		options.PrefetchValues = false

		iterator := transaction.NewIterator(options)
		defer iterator.Close()

		for iterator.Seek(seek); iterator.ValidForPrefix(options.Prefix); iterator.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}

			key := iterator.Item().KeyCopy(nil)

			keys = append(keys, key)
			if len(keys) == startupActiveValidationBatch {
				next = append(key, 0)

				break
			}
		}

		return nil
	})

	return keys, next, err
}

func (store *Store) validateActiveKey(ctx context.Context, key []byte) error {
	return store.db.View(func(transaction *badger.Txn) error {
		item, err := transaction.Get(key)
		if err != nil {
			return err
		}

		pointer, err := decodeActiveItem(item)
		if err != nil {
			return err
		}

		_, found, err := loadActiveState(ctx, transaction, scopeFromPointer(pointer))
		if err != nil {
			return err
		}

		if !found {
			return ErrCorrupt
		}

		return nil
	})
}

// CollectGarbage removes unreachable generations. Existing snapshots are
// independent deep copies and remain valid after collection.
func (store *Store) CollectGarbage(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}

	unlock, err := store.gate.acquire(ctx, true)
	if err != nil {
		return err
	}
	defer unlock()

	if err := store.ensureOpen(); err != nil {
		return err
	}

	return store.collectGarbage(ctx)
}

func scopeFromPointer(pointer activePointer) memory.Scope {
	return memory.Scope{Key: pointer.ScopeKey, Kind: memory.ScopeKind(pointer.ScopeKind)}
}

func (store *Store) collectGarbage(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := store.deleteGarbagePrefix(ctx, statePrefix(), garbageState); err != nil {
		return err
	}

	if err := store.deleteGarbagePrefix(ctx, legacyUsedBuildPrefix(), garbageLegacyClaim); err != nil {
		return err
	}

	return store.deleteGarbagePrefix(ctx, allChunkPrefix(), garbageChunk)
}

func decodeActiveItem(item *badger.Item) (activePointer, error) {
	value, err := readItemValueBounded(item, maxActiveValueBytes)
	if err != nil {
		return activePointer{}, err
	}

	var pointer activePointer
	if err := decodeJSON(value, &pointer); err != nil || pointer.SchemaVersion != schemaVersion {
		return activePointer{}, ErrCorrupt
	}

	scope := scopeFromPointer(pointer)
	if validateScope(scope) != nil || !validIdentity(pointer.BuildID) || pointer.Epoch == 0 {
		return activePointer{}, ErrCorrupt
	}

	if !bytes.Equal(item.Key(), activeKey(scope)) {
		return activePointer{}, ErrCorrupt
	}

	return pointer, nil
}

func (store *Store) deleteGarbagePrefix(ctx context.Context, prefix []byte, kind garbageRecordKind) error {
	seek := append([]byte(nil), prefix...)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		deletions, next, scanned, err := store.scanGarbageBatch(ctx, prefix, seek, kind)
		if err != nil {
			return err
		}

		if store.afterGarbageScanBatch != nil && scanned > 0 {
			store.afterGarbageScanBatch(scanned)
		}

		if len(deletions) > 0 {
			if err := store.deleteKeyBatch(ctx, deletions); err != nil {
				return err
			}

			if store.afterGarbageDeleteBatch != nil {
				store.afterGarbageDeleteBatch(len(deletions))
			}
		}

		if next == nil {
			return nil
		}

		seek = next
	}
}

func (store *Store) scanGarbageBatch(
	ctx context.Context,
	prefix []byte,
	seek []byte,
	kind garbageRecordKind,
) (deletions [][]byte, next []byte, scanned int, err error) {
	err = store.db.View(func(transaction *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix

		iterator := transaction.NewIterator(options)
		defer iterator.Close()

		classifier := garbageBatchClassifier{
			transaction: transaction, activeByScope: make(map[string]garbageActiveGeneration),
		}

		for iterator.Seek(seek); iterator.ValidForPrefix(prefix); iterator.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}

			key := iterator.Item().KeyCopy(nil)
			scanned++

			keep, classifyErr := classifier.keep(key, kind)
			if classifyErr != nil {
				return classifyErr
			}

			if !keep {
				deletions = append(deletions, key)
			}

			if scanned == garbageDeleteBatch {
				next = append(key, 0)

				break
			}
		}

		return nil
	})

	return deletions, next, scanned, err
}

func (store *Store) deleteKeyBatch(ctx context.Context, keys [][]byte) error {
	if err := store.db.Update(func(transaction *badger.Txn) error {
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return err
			}

			if err := transaction.Delete(key); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("delete unreachable semantic generation: %w", err)
	}

	return nil
}
