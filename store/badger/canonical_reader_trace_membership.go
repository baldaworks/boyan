package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func traceMembershipUnavailable() error {
	return memory.PermanentError(memory.CodeForgotten, "trace source membership is unavailable", nil)
}

func loadTraceMessageIDs(txn *badger.Txn, scope memory.Scope, sourceID string) ([]string, error) {
	source, found, err := loadHistoricalPrivacySource(txn, scope, sourceID)
	if err != nil {
		return nil, err
	}

	if !found {
		return nil, traceMembershipUnavailable()
	}

	certificate, certified, err := loadHistoricalTurnCertificate(txn, scope, sourceID)
	if err != nil {
		return nil, err
	}

	if certified {
		if err := validateTraceCertificateSource(txn, source, certificate); err != nil {
			return nil, err
		}

		return certificate.MessageIDs, nil
	}

	record, exists, err := loadHistoricalSourcePrivacy(txn, source)
	if err != nil {
		return nil, err
	}

	return tracePreparedMessageIDs(record, exists)
}

func validateTraceMessageMember(record memory.MessageRecord, sourceID, id string) error {
	if record.Validate() != nil || record.MessageID != id || record.SourceID != sourceID {
		return invalidSourcePrivacy()
	}

	if record.Payload.ByteSize > memory.MaxDerivedTurnTextBytes {
		return memory.PermanentError(memory.CodeLimitExceeded, "trace message payload exceeds its bound", nil)
	}

	return nil
}

func validateTraceCertificateSource(txn *badger.Txn, source memory.CanonicalSourceRecord,
	certificate memory.CanonicalTurnCertificate) error {
	payload, err := readPrivacySourcePayload(txn, source)
	if err != nil {
		return err
	}

	native, err := decodeCanonicalTraceTurnPayload(payload)
	if err != nil {
		return traceMembershipUnavailable()
	}

	if certificate.SourceID != source.SourceID || source.SourceID != memory.TurnSourceID(native.ExportID) {
		return invalidSourcePrivacy()
	}

	return nil
}

func tracePreparedMessageIDs(record sourcePrivacy, exists bool) ([]string, error) {
	if !exists || record.Preparing || !record.MessageMembershipComplete || len(record.MessageIDs) == 0 {
		return nil, traceMembershipUnavailable()
	}

	return record.MessageIDs, nil
}
