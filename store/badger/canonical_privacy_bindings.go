package badger

import (
	"encoding/json"
	"errors"
	"slices"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func validateStoredPrivacyBindings(txn *badger.Txn, components []string, envelope memory.RecordEnvelope) error {
	switch envelope.RecordType {
	case badgerRecordSourcePrivacy:
		return validatePrivacyAggregateBindings(txn, decodePrivacyAggregate(envelope))
	case badgerRecordMessage:
		return validatePrivacyMessageCapture(txn, components, envelope)
	case badgerRecordTurnCertificate:
		return validatePrivacyCertificateBindings(txn, components, envelope)
	default:
		return nil
	}
}

func validatePrivacyAggregateBindings(txn *badger.Txn, record sourcePrivacy) error {
	if errors.Is(checkDeniedSource(txn, record.Scope, record.SourceID), errCanonicalDenied) {
		return nil
	}

	source, found, err := loadHistoricalPrivacySource(txn, record.Scope, record.SourceID)
	if err != nil {
		return err
	}

	if !found || !sourcePrivacyBindingsMatch(record, source) {
		return invalidSourcePrivacy()
	}

	if err := validateCompletePrivacyCapture(txn, source, record); err != nil {
		return err
	}

	for _, id := range record.MessageIDs {
		if err := validatePrivacyMemberBinding(txn, source, id); err != nil {
			return err
		}
	}

	return nil
}

func validatePrivacyMemberBinding(txn *badger.Txn, source memory.CanonicalSourceRecord, id string) error {
	key, err := canonicalKey(source.Scope, badgerRecordMessage, id)
	if err != nil {
		return err
	}

	var message memory.MessageRecord

	err = getPrivacyPreparationRecord(txn, key, badgerRecordMessage, &message)
	if privacyMissing(err) {
		return validatePrivacyEmbeddedMember(txn, source, id)
	}

	if err != nil {
		return err
	}

	if message.Validate() != nil || message.SourceID != source.SourceID || message.MessageID != id {
		return invalidSourcePrivacy()
	}

	complete, err := historicalMessageContentComplete(txn, source, message)
	if err != nil {
		return err
	}

	if !complete {
		return invalidSourcePrivacy()
	}

	return nil
}

func validatePrivacyEmbeddedMember(txn *badger.Txn, source memory.CanonicalSourceRecord, id string) error {
	data, err := readPrivacySourcePayload(txn, source)
	if err != nil {
		return err
	}

	var embedded memory.SourceRecord
	if json.Unmarshal(data, &embedded) != nil {
		return invalidSourcePrivacy()
	}

	valid, err := historicalEmbeddedSourceValid(source, embedded)
	if err != nil {
		return err
	}

	if !valid {
		return invalidSourcePrivacy()
	}

	for _, message := range embedded.Turn.Messages {
		if message.MessageID == id {
			return nil
		}
	}

	return invalidSourcePrivacy()
}

func validatePrivacyMessageCapture(txn *badger.Txn, components []string, envelope memory.RecordEnvelope) error {
	if len(components) != canonicalRecordComponents {
		return invalidSourcePrivacy()
	}

	var message memory.MessageRecord
	if json.Unmarshal(envelope.Payload, &message) != nil ||
		message.Validate() != nil || message.MessageID != components[3] {
		return invalidSourcePrivacy()
	}

	scope := memory.Scope{Key: components[1], Kind: memory.ScopeKind(components[2])}

	source, found, err := loadHistoricalPrivacySource(txn, scope, message.SourceID)
	if err != nil || !found {
		return err
	}

	if err := validatePrivacyCertifiedMember(txn, source, message.MessageID); err != nil {
		return err
	}

	return validatePrivacyCapturedMember(txn, source, message.MessageID)
}

func validatePrivacyCertifiedMember(txn *badger.Txn, source memory.CanonicalSourceRecord, id string) error {
	certificate, found, err := loadHistoricalTurnCertificate(txn, source.Scope, source.SourceID)
	if err != nil {
		return err
	}

	if found && !slices.Contains(certificate.MessageIDs, id) {
		return invalidSourcePrivacy()
	}

	return nil
}

func validatePrivacyCapturedMember(txn *badger.Txn, source memory.CanonicalSourceRecord, id string) error {
	record, found, err := loadHistoricalSourcePrivacy(txn, source)
	if err != nil {
		return err
	}

	if found && !record.Preparing && record.MessageMembershipComplete && !slices.Contains(record.MessageIDs, id) {
		return invalidSourcePrivacy()
	}

	return nil
}

func validatePrivacyCertificateBindings(txn *badger.Txn, components []string, envelope memory.RecordEnvelope) error {
	if len(components) != canonicalRecordComponents {
		return invalidSourcePrivacy()
	}

	certificate, err := decodePrivacyCertificate(envelope.Payload, components[3])
	if err != nil {
		return err
	}

	scope := memory.Scope{Key: components[1], Kind: memory.ScopeKind(components[2])}
	if errors.Is(checkDeniedSource(txn, scope, certificate.SourceID), errCanonicalDenied) {
		return nil
	}

	source, found, err := loadHistoricalPrivacySource(txn, scope, certificate.SourceID)
	if err != nil {
		return err
	}

	if !found {
		return invalidSourcePrivacy()
	}

	if err := validatePrivacyCertificateSource(txn, source, certificate); err != nil {
		return err
	}

	for _, id := range certificate.MessageIDs {
		if err := validatePrivacyMemberBinding(txn, source, id); err != nil {
			return err
		}
	}

	return nil
}

func validatePrivacyCertificateSource(
	txn *badger.Txn, source memory.CanonicalSourceRecord, certificate memory.CanonicalTurnCertificate,
) error {
	data, err := readPrivacySourcePayload(txn, source)
	if err != nil {
		return err
	}

	native, err := decodeCanonicalTraceTurnPayload(data)
	if err != nil || source.SourceID != memory.TurnSourceID(native.ExportID) || certificate.SourceID != source.SourceID {
		return invalidSourcePrivacy()
	}

	return nil
}

func validateCompletePrivacyCapture(txn *badger.Txn, source memory.CanonicalSourceRecord, record sourcePrivacy) error {
	if record.Preparing || !record.MessageMembershipComplete {
		return nil
	}

	certificate, found, err := loadHistoricalTurnCertificate(txn, source.Scope, source.SourceID)
	if err != nil {
		return err
	}

	if found {
		if !samePrivacyMemberIDs(record.MessageIDs, certificate.MessageIDs) {
			return invalidSourcePrivacy()
		}

		return nil
	}

	return validateCompleteEmbeddedCapture(txn, source, record.MessageIDs)
}

func validateCompleteEmbeddedCapture(
	txn *badger.Txn, source memory.CanonicalSourceRecord, captured []string,
) error {
	data, err := readPrivacySourcePayload(txn, source)
	if err != nil {
		return err
	}

	var embedded memory.SourceRecord
	if json.Unmarshal(data, &embedded) != nil || embedded.Ref.Scope.Key == "" {
		return nil
	}

	valid, err := historicalEmbeddedSourceValid(source, embedded)
	if err != nil {
		return err
	}

	if !valid {
		return invalidSourcePrivacy()
	}

	ids := make([]string, 0, len(embedded.Turn.Messages))
	for _, message := range embedded.Turn.Messages {
		ids = append(ids, message.MessageID)
	}

	if !samePrivacyMemberIDs(captured, ids) {
		return invalidSourcePrivacy()
	}

	return nil
}

func samePrivacyMemberIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	for _, id := range right {
		if !slices.Contains(left, id) {
			return false
		}
	}

	return true
}

func decodePrivacyCertificate(payload []byte, id string) (memory.CanonicalTurnCertificate, error) {
	var certificate memory.CanonicalTurnCertificate
	if decodeCanonicalProofJSON(payload, &certificate) != nil ||
		certificate.Validate() != nil || certificate.SourceID != id {
		return certificate, invalidSourcePrivacy()
	}

	return certificate, nil
}
