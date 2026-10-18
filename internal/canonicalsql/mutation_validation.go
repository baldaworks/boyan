package canonicalsql

import (
	"context"

	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validateMutationReferences(ctx context.Context, txn *Tx, mutation memory.CanonicalMutation) error {
	items := make(map[string]struct{}, len(mutation.Items))
	for _, item := range mutation.Items {
		items[item.ItemID] = struct{}{}
	}

	revisions := make(map[string]struct{}, len(mutation.Revisions))
	for _, revision := range mutation.Revisions {
		revisions[revision.RevisionID] = struct{}{}
		if _, exists := items[revision.ItemID]; !exists {
			if err := mutationRecordExists(ctx, txn, mutation.Scope, Item, revision.ItemID); err != nil {
				return fmt.Errorf("revision item reference: %w", err)
			}
		}
	}

	return validateMutationRevisionReferences(ctx, txn, mutation, revisions)
}

func validateMutationRevisionReferences(
	ctx context.Context, txn *Tx,
	mutation memory.CanonicalMutation,
	revisions map[string]struct{},
) error {
	for _, lifecycle := range mutation.Lifecycle {
		if _, exists := revisions[lifecycle.RevisionID]; !exists {
			if err := mutationRecordExists(ctx, txn, mutation.Scope, Revision, lifecycle.RevisionID); err != nil {
				return fmt.Errorf("lifecycle revision reference: %w", err)
			}
		}
	}

	for _, head := range mutation.Heads {
		if _, exists := revisions[head.RevisionID]; !exists {
			if err := mutationRecordExists(ctx, txn, mutation.Scope, Revision, head.RevisionID); err != nil {
				return fmt.Errorf("head revision reference: %w", err)
			}
		}
	}

	return nil
}

func mutationRecordExists(ctx context.Context, txn *Tx, scope memory.Scope, recordType Family, id string) error {
	key, err := privacyKey(scope, recordType, id)
	if err != nil {
		return err
	}

	if _, err := txn.Get(ctx, key); privacyMissing(err) {
		return memory.PermanentError(memory.CodeConflict, "canonical record reference does not exist", nil)
	} else if err != nil {
		return err
	}

	return nil
}
