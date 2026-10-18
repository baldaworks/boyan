package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func initializeNewPrivacyMembership(ctx context.Context, txn *Tx, mutation memory.CanonicalMutation,
	fresh map[string]bool) error {
	members := make(map[string][]string, len(mutation.Sources))
	overflow := make(map[string]bool)

	for _, message := range mutation.Messages {
		if !fresh[message.SourceID] {
			continue
		}

		if len(members[message.SourceID]) >= sourcePrivacyBatchSize {
			overflow[message.SourceID] = true

			continue
		}

		members[message.SourceID] = append(members[message.SourceID], message.MessageID)
	}

	for _, source := range mutation.Sources {
		if !fresh[source.SourceID] {
			continue
		}

		if err := writeInitialPrivacyMembership(ctx, txn, source,
			members[source.SourceID], overflow[source.SourceID]); err != nil {
			return err
		}
	}

	return nil
}

func writeInitialPrivacyMembership(
	ctx context.Context, txn *Tx,
	source memory.CanonicalSourceRecord,
	ids []string,
	overflow bool,
) error {
	record, found, err := loadSourcePrivacy(ctx, txn, source)
	if err != nil {
		return err
	}

	if !found {
		return invalidSourcePrivacy()
	}

	_, certified, err := loadTurnCertificate(ctx, txn, source.Scope, source.SourceID)
	if err != nil {
		return err
	}

	if !certified {
		record.MessageIDs = ids

		complete, err := initialPrivacyMembershipComplete(ctx, txn, source.Scope, ids, overflow)
		if err != nil {
			return err
		}

		record.MessageMembershipComplete = complete
	}

	if overflow && record.State != sourcePrivacySensitive {
		record.State = sourcePrivacyUnknown
	}

	return writeSourcePrivacy(ctx, txn, record)
}

func initialPrivacyMembershipComplete(ctx context.Context, txn *Tx, scope memory.Scope, ids []string,
	overflow bool) (bool, error) {
	incomplete, err := privacyScopeIncomplete(ctx, txn, scope)
	if err != nil {
		return false, err
	}

	return !incomplete && !overflow && len(ids) > 0, nil
}

func completeMutationPrivacyMembership(ctx context.Context, txn *Tx, mutation memory.CanonicalMutation,
	fresh, extensions map[string]bool) error {
	for sourceID, extended := range extensions {
		if extended && !fresh[sourceID] {
			if err := invalidatePrivacyMembership(ctx, txn, mutation.Scope, sourceID); err != nil {
				return err
			}
		}
	}

	return initializeNewPrivacyMembership(ctx, txn, mutation, fresh)
}

func invalidatePrivacyMembership(ctx context.Context, txn *Tx, scope memory.Scope, sourceID string) error {
	source, found, err := loadPrivacySource(ctx, txn, scope, sourceID)
	if err != nil || !found {
		return err
	}

	record, exists, err := loadSourcePrivacy(ctx, txn, source)
	if err != nil {
		return err
	}

	if !exists {
		return invalidSourcePrivacy()
	}

	record.MessageMembershipComplete = false

	return writeSourcePrivacy(ctx, txn, record)
}
