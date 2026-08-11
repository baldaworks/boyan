package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) putCanonicalMutationRecords(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	if err := validateBadgerMutationReferences(txn, mutation); err != nil {
		return err
	}
	if err := validateBadgerMutationProvenance(txn, mutation); err != nil {
		return err
	}
	if err := putCanonicalMutationPayloads(txn, mutation.Payloads); err != nil {
		return err
	}
	if err := putCanonicalMutationCoreRecords(txn, mutation.Scope, mutation.Sources, mutation.Messages, mutation.Items); err != nil {
		return err
	}
	if err := putCanonicalMutationRevisions(txn, mutation.Scope, mutation.Revisions); err != nil {
		return err
	}
	return putCanonicalMutationTailRecords(txn, mutation.Scope, mutation.Lifecycle, mutation.Heads, mutation.Delivery)
}
