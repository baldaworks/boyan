package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validateMutationProvenance(ctx context.Context, txn *Tx, mutation memory.CanonicalMutation) error {
	revisions := make(map[string]memory.MemoryRevision, len(mutation.Revisions))
	for _, revision := range mutation.Revisions {
		revisions[revision.RevisionID] = revision
	}

	if err := validateMutationParents(ctx, txn, mutation.Scope, mutation.Revisions, revisions); err != nil {
		return err
	}

	return validateMutationProvenanceCycles(revisions)
}
