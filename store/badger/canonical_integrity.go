package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// VerifyCanonicalIntegrity checks all application-owned record envelopes and
// their checksums. It does not attempt destructive repair or decrypt payloads.
func (s *CanonicalStore) VerifyCanonicalIntegrity(ctx context.Context) error {
	if err := checkCanonicalMaintenanceContext(ctx); err != nil {
		return err
	}

	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()

	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	err := s.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()

		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			if err := checkCanonicalMaintenanceContext(ctx); err != nil {
				return err
			}

			item := iterator.Item()

			encoded, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			if _, err := memory.UnmarshalRecordEnvelope(encoded); err != nil {
				return memory.PermanentError(memory.CodeStoreFailure, "canonical record integrity check failed", err)
			}
		}

		return nil
	})
	if err != nil {
		if _, _, ok := memory.ClassifyError(err); ok {
			return err
		}

		return canonicalStoreError("verify canonical integrity", err)
	}

	return nil
}
