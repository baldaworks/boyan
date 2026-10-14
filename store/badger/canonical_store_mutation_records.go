package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) putCanonicalMutationRecords(ctx context.Context,
	txn *badger.Txn, mutation memory.CanonicalMutation) error {
	if err := validateCanonicalBoundaryGrounding(ctx, txn, mutation); err != nil {
		return err
	}

	if err := validateEraseMutationPayloadLocks(txn, mutation); err != nil {
		return err
	}

	if err := validateBadgerMutationReferences(txn, mutation); err != nil {
		return err
	}

	if err := validateBadgerMutationProvenance(txn, mutation); err != nil {
		return err
	}

	freshPrivacySources, extensions, err := capturePrivacyMembership(txn, mutation)
	if err != nil {
		return err
	}

	if err := putCanonicalMutationPayloads(txn, mutation.Payloads); err != nil {
		return err
	}

	if err := putCanonicalMutationCoreRecords(
		txn,
		mutation.Scope,
		mutation.Sources,
		mutation.Messages,
		mutation.Items,
	); err != nil {
		return err
	}

	if err := putCanonicalMutationRevisions(txn, mutation.Scope, mutation.Revisions); err != nil {
		return err
	}

	if err := mergeMutationSourcePrivacy(txn, mutation, freshPrivacySources, extensions); err != nil {
		return err
	}

	return putCanonicalMutationTailRecords(txn, mutation.Scope, mutation.Lifecycle, mutation.Heads, mutation.Delivery)
}

func validateEraseMutationPayloadLocks(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	for _, source := range mutation.Sources {
		if err := checkEraseBlobWrite(txn, source.Payload.ID); err != nil {
			return err
		}
	}

	for _, message := range mutation.Messages {
		if err := checkEraseBlobWrite(txn, message.Payload.ID); err != nil {
			return err
		}
	}

	for _, revision := range mutation.Revisions {
		if err := checkEraseBlobWrite(txn, revision.Payload.ID); err != nil {
			return err
		}
	}

	for _, payload := range mutation.Payloads {
		if err := checkEraseBlobWrite(txn, payload.Ref.ID); err != nil {
			return err
		}
	}

	return nil
}
