package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func initializeNewPrivacyMembership(txn *badger.Txn, mutation memory.CanonicalMutation, fresh map[string]bool) error {
	members := make(map[string][]string, len(mutation.Sources))
	overflow := make(map[string]bool)

	for _, message := range mutation.Messages {
		if !fresh[message.SourceID] {
			continue
		}

		if len(members[message.SourceID]) >= maxCanonicalTraceSourceMessages {
			overflow[message.SourceID] = true

			continue
		}

		members[message.SourceID] = append(members[message.SourceID], message.MessageID)
	}

	for _, source := range mutation.Sources {
		if !fresh[source.SourceID] {
			continue
		}

		if err := writeInitialPrivacyMembership(txn, source,
			members[source.SourceID], overflow[source.SourceID]); err != nil {
			return err
		}
	}

	return nil
}

func writeInitialPrivacyMembership(
	txn *badger.Txn,
	source memory.CanonicalSourceRecord,
	ids []string,
	overflow bool,
) error {
	record, found, err := loadSourcePrivacy(txn, source)
	if err != nil {
		return err
	}

	if !found {
		return invalidSourcePrivacy()
	}

	_, certified, err := loadTurnCertificate(txn, source.Scope, source.SourceID)
	if err != nil {
		return err
	}

	if !certified {
		record.MessageIDs = ids

		complete, err := initialPrivacyMembershipComplete(txn, source.Scope, ids, overflow)
		if err != nil {
			return err
		}

		record.MessageMembershipComplete = complete
	}

	if overflow && record.State != sourcePrivacySensitive {
		record.State = sourcePrivacyUnknown
	}

	return writeSourcePrivacy(txn, record)
}

func validPrivacyMembership(ids []string) bool {
	if len(ids) > maxCanonicalTraceSourceMessages {
		return false
	}

	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		ref := memory.PayloadRef{ID: id, Digest: "privacy", ByteSize: 1}
		if ref.Validate() != nil || seen[id] {
			return false
		}

		seen[id] = true
	}

	return true
}

func initialPrivacyMembershipComplete(txn *badger.Txn, scope memory.Scope, ids []string, overflow bool) (bool, error) {
	incomplete, err := privacyScopeIncomplete(txn, scope)
	if err != nil {
		return false, err
	}

	return !incomplete && !overflow && len(ids) > 0, nil
}

func completeMutationPrivacyMembership(txn *badger.Txn, mutation memory.CanonicalMutation,
	fresh, extensions map[string]bool) error {
	for sourceID, extended := range extensions {
		if extended && !fresh[sourceID] {
			if err := invalidatePrivacyMembership(txn, mutation.Scope, sourceID); err != nil {
				return err
			}
		}
	}

	return initializeNewPrivacyMembership(txn, mutation, fresh)
}

func invalidatePrivacyMembership(txn *badger.Txn, scope memory.Scope, sourceID string) error {
	source, found, err := loadPrivacySource(txn, scope, sourceID)
	if err != nil || !found {
		return err
	}

	record, exists, err := loadSourcePrivacy(txn, source)
	if err != nil {
		return err
	}

	if !exists {
		return invalidSourcePrivacy()
	}

	record.MessageMembershipComplete = false

	return writeSourcePrivacy(txn, record)
}
