package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// A denied head must not become the parent of newly written memory. Compare
// the revision so replay of an old outcome never removes a later replacement.
func eraseRemoveHead(ctx context.Context, txn *Tx, scope memory.Scope, revision memory.MemoryRevision) error {
	key, err := eraseKey(scope, Head, revision.ItemID)
	if err != nil {
		return err
	}

	var head memory.ItemHead

	err = getPrivacyRecord(ctx, txn, key, Head, &head)
	if privacyMissing(err) {
		return nil
	}

	if err != nil {
		return err
	}

	if err := head.Validate(); err != nil {
		return eraseStoreFailure("invalid scope erase target head", err)
	}

	if head.ItemID != revision.ItemID {
		return eraseStoreFailure("scope erase target head item differs", nil)
	}

	if head.RevisionID != revision.RevisionID {
		return nil
	}

	return txn.Delete(ctx, key)
}
