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
