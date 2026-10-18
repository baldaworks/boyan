package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validateMutationParents(
	ctx context.Context, txn *Tx,
	scope memory.Scope,
	revisions []memory.MemoryRevision,
	staged map[string]memory.MemoryRevision,
) error {
	for _, revision := range revisions {
		for _, parentID := range revision.Parents {
			if _, exists := staged[parentID]; exists {
				continue
			}

			parentKey, err := privacyKey(scope, Revision, parentID)
			if err != nil {
				return err
			}

			if _, err := txn.Get(ctx, parentKey); privacyMissing(err) {
				return memory.PermanentError(memory.CodeConflict, "canonical provenance parent does not exist", nil)
			} else if err != nil {
				return err
			}
		}
	}

	return nil
}
