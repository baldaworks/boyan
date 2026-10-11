package badger

import (
	"errors"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func validateBadgerMutationReferences(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	items := make(map[string]struct{}, len(mutation.Items))
	for _, item := range mutation.Items {
		items[item.ItemID] = struct{}{}
	}

	revisions := make(map[string]struct{}, len(mutation.Revisions))
	for _, revision := range mutation.Revisions {
		revisions[revision.RevisionID] = struct{}{}
		if _, exists := items[revision.ItemID]; !exists {
			if err := badgerCanonicalRecordExists(txn, mutation.Scope, badgerRecordItem, revision.ItemID); err != nil {
				return fmt.Errorf("revision item reference: %w", err)
			}
		}
	}

	return validateBadgerMutationRevisionReferences(txn, mutation, revisions)
}

func validateBadgerMutationRevisionReferences(
	txn *badger.Txn,
	mutation memory.CanonicalMutation,
	revisions map[string]struct{},
) error {
	for _, lifecycle := range mutation.Lifecycle {
		if _, exists := revisions[lifecycle.RevisionID]; !exists {
			if err := badgerCanonicalRecordExists(txn, mutation.Scope, badgerRecordRevision, lifecycle.RevisionID); err != nil {
				return fmt.Errorf("lifecycle revision reference: %w", err)
			}
		}
	}

	for _, head := range mutation.Heads {
		if _, exists := revisions[head.RevisionID]; !exists {
			if err := badgerCanonicalRecordExists(txn, mutation.Scope, badgerRecordRevision, head.RevisionID); err != nil {
				return fmt.Errorf("head revision reference: %w", err)
			}
		}
	}

	return nil
}

func badgerCanonicalRecordExists(txn *badger.Txn, scope memory.Scope, recordType, id string) error {
	key, err := canonicalKey(scope, recordType, id)
	if err != nil {
		return err
	}

	if _, err := txn.Get(key); errors.Is(err, badger.ErrKeyNotFound) {
		return memory.PermanentError(memory.CodeConflict, "canonical record reference does not exist", nil)
	} else if err != nil {
		return err
	}

	return nil
}
