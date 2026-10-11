package badger

import (
	"errors"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func activateProjectionGenerationTx(
	txn *badger.Txn,
	manifest memory.ProjectionManifest,
	manifestKey []byte,
	activeKey []byte,
	updatedAt time.Time,
) error {
	var stored memory.ProjectionManifest
	if err := getCanonicalRecord(txn, manifestKey, badgerRecordProjectionManifest, &stored); err != nil {
		return err
	}

	if stored.Status != memory.ProjectionGenerationDirty {
		return memory.PermanentError(memory.CodeConflict, "projection generation is not dirty", nil)
	}

	if err := supersedePreviousProjectionGeneration(txn, manifest, activeKey, stored.GenerationID, updatedAt); err != nil {
		return err
	}

	stored.Status = memory.ProjectionGenerationActive

	stored.UpdatedAt = updatedAt.UTC()
	if err := putCanonicalRecord(txn, manifestKey, badgerRecordProjectionManifest, stored); err != nil {
		return err
	}

	return putCanonicalRecord(
		txn,
		activeKey,
		badgerRecordProjectionActive,
		badgerProjectionActive{GenerationID: stored.GenerationID},
	)
}

func supersedePreviousProjectionGeneration(
	txn *badger.Txn,
	manifest memory.ProjectionManifest,
	activeKey []byte,
	currentGenerationID string,
	updatedAt time.Time,
) error {
	var previous badgerProjectionActive
	if err := getCanonicalRecord(txn, activeKey, badgerRecordProjectionActive, &previous); err == nil &&
		previous.GenerationID != currentGenerationID {
		previousKey, keyErr := badgerProjectionManifestKey(manifest.Scope, manifest.ProjectionID, previous.GenerationID)
		if keyErr != nil {
			return keyErr
		}

		var old memory.ProjectionManifest
		if err := getCanonicalRecord(txn, previousKey, badgerRecordProjectionManifest, &old); err != nil {
			return err
		}

		old.Status = memory.ProjectionGenerationSuperseded
		old.UpdatedAt = updatedAt.UTC()

		return putCanonicalRecord(txn, previousKey, badgerRecordProjectionManifest, old)
	} else if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}

	return nil
}
