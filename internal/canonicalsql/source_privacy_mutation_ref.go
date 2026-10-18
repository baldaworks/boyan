package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validatePrivacyEvidenceRef(ctx context.Context, txn *Tx, scope memory.Scope,
	ref memory.EvidenceRef) (bool, error) {
	source, found, err := loadPrivacySource(ctx, txn, scope, ref.SourceID)
	if err != nil || !found {
		return false, err
	}

	key, err := privacyKey(scope, Message, ref.MessageID)
	if err != nil {
		return false, err
	}

	var message memory.MessageRecord

	err = getPrivacyRecord(ctx, txn, key, Message, &message)
	if privacyMissing(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	valid, err := validatePrivacyMessage(message, ref)
	if err != nil || !valid {
		return false, err
	}

	return validatePrivacyRawEvidence(ctx, txn, source, ref)
}
