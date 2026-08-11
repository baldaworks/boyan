package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func validateBadgerMutationProvenance(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	revisions := make(map[string]memory.MemoryRevision, len(mutation.Revisions))
	for _, revision := range mutation.Revisions {
		revisions[revision.RevisionID] = revision
	}
	if err := validateBadgerMutationParents(txn, mutation.Scope, mutation.Revisions, revisions); err != nil {
		return err
	}
	return validateBadgerMutationProvenanceCycles(revisions)
}
