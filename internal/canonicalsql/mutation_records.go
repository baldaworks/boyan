package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func putCanonicalMutationRecords(ctx context.Context,
	txn *Tx, mutation memory.CanonicalMutation) error {
	if err := validateCanonicalBoundaryGrounding(ctx, txn, mutation); err != nil {
		return err
	}

	if err := validateEraseMutationPayloadLocks(ctx, txn, mutation); err != nil {
		return err
	}

	if err := validateMutationReferences(ctx, txn, mutation); err != nil {
		return err
	}

	if err := validateMutationProvenance(ctx, txn, mutation); err != nil {
		return err
	}

	freshPrivacySources, extensions, err := capturePrivacyMembership(ctx, txn, mutation)
	if err != nil {
		return err
	}

	if err := putCanonicalMutationPayloads(ctx, txn, mutation.Payloads); err != nil {
		return err
	}

	if err := putCanonicalMutationCoreRecords(
		ctx,
		txn,
		mutation.Scope,
		mutation.Sources,
		mutation.Messages,
		mutation.Items,
	); err != nil {
		return err
	}

	if err := putCanonicalMutationRevisions(ctx, txn, mutation.Scope, mutation.Revisions); err != nil {
		return err
	}

	if err := mergeMutationSourcePrivacy(ctx, txn, mutation, freshPrivacySources, extensions); err != nil {
		return err
	}

	return putCanonicalMutationTailRecords(ctx, txn, mutation.Scope, mutation.Lifecycle, mutation.Heads, mutation.Delivery)
}

func validateEraseMutationPayloadLocks(ctx context.Context, txn *Tx, mutation memory.CanonicalMutation) error {
	for _, source := range mutation.Sources {
		if err := checkEraseBlobWrite(ctx, txn, source.Payload.ID); err != nil {
			return err
		}
	}

	for _, message := range mutation.Messages {
		if err := checkEraseBlobWrite(ctx, txn, message.Payload.ID); err != nil {
			return err
		}
	}

	for _, revision := range mutation.Revisions {
		if err := checkEraseBlobWrite(ctx, txn, revision.Payload.ID); err != nil {
			return err
		}
	}

	for _, payload := range mutation.Payloads {
		if err := checkEraseBlobWrite(ctx, txn, payload.Ref.ID); err != nil {
			return err
		}
	}

	return nil
}
