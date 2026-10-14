package badger

import (
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadHistoricalPrivacySource(
	txn *badger.Txn,
	scope memory.Scope,
	id string,
) (memory.CanonicalSourceRecord, bool, error) {
	key, err := canonicalKey(scope, badgerRecordSource, id)
	if err != nil {
		return memory.CanonicalSourceRecord{}, false, err
	}

	var source memory.CanonicalSourceRecord

	err = getPrivacyPreparationRecord(txn, key, badgerRecordSource, &source)
	if privacyMissing(err) {
		return source, false, nil
	}

	if err != nil {
		return source, false, err
	}

	normalized := source
	normalized.Sensitivity = memory.SensitivityStandard

	normalized.Retention = memory.RetentionClassStandard
	if normalized.Validate() != nil || source.Scope != scope || source.SourceID != id {
		return source, false, invalidSourcePrivacy()
	}

	return source, true, nil
}

func (s *CanonicalStore) initializeHistoricalSourcePrivacy(
	txn *badger.Txn,
	_ []byte,
	scope memory.Scope,
	id string,
) error {
	source, _, err := loadHistoricalPrivacySource(txn, scope, id)
	if err != nil {
		return err
	}

	previous, found, err := loadHistoricalSourcePrivacy(txn, source)
	if err != nil {
		return err
	}

	valid, err := historicalSourceRepresentable(txn, source)
	if err != nil {
		return err
	}

	record := initialHistoricalPrivacyRecord(source, previous, found, valid)
	if err := writeSourcePrivacy(txn, record); err != nil {
		return err
	}

	return putPrivacyScope(txn, scope, false)
}

func historicalSourceRepresentable(txn *badger.Txn, source memory.CanonicalSourceRecord) (bool, error) {
	payload, err := readPrivacySourcePayload(txn, source)
	if err != nil || payload == nil {
		return false, err
	}

	var embedded memory.SourceRecord
	if json.Unmarshal(payload, &embedded) == nil && embedded.Ref.Scope.Key != "" {
		return historicalEmbeddedSourceValid(source, embedded)
	}

	return validPrivacyNativePayload(payload, source.Scope), nil
}

func historicalEmbeddedSourceValid(source memory.CanonicalSourceRecord, embedded memory.SourceRecord) (bool, error) {
	if embedded.Ref.Scope != source.Scope {
		return false, invalidSourcePrivacy()
	}

	if embedded.SchemaVersion != memory.DerivedSchemaVersionV1 || embedded.State != memory.SourceStateActive {
		return false, nil
	}

	if embedded.Validate() != nil {
		return false, nil
	}

	if source.SourceID != embedded.Turn.SourceID && source.SourceID != embedded.Ref.ExportID {
		return false, invalidSourcePrivacy()
	}

	return true, nil
}

func (s *CanonicalStore) collectHistoricalPrivacyMessage(
	txn *badger.Txn,
	key []byte,
	scope memory.Scope,
	id string,
) error {
	var message memory.MessageRecord
	if err := getPrivacyPreparationRecord(txn, key, badgerRecordMessage, &message); err != nil {
		return err
	}

	if message.MessageID != id || message.Validate() != nil {
		return invalidSourcePrivacy()
	}

	source, found, err := loadHistoricalPrivacySource(txn, scope, message.SourceID)
	if err != nil {
		return err
	}

	if !found {
		return putPrivacyScope(txn, scope, true)
	}

	record, exists, err := loadHistoricalSourcePrivacy(txn, source)
	if err != nil {
		return err
	}

	if !exists {
		return invalidSourcePrivacy()
	}

	return mergeHistoricalMessageMembership(txn, source, record, message)
}

func mergeHistoricalMessageMembership(txn *badger.Txn, source memory.CanonicalSourceRecord,
	record sourcePrivacy, message memory.MessageRecord) error {
	content, err := historicalMessageContentComplete(txn, source, message)
	if err != nil {
		return err
	}

	record.Incomplete = record.Incomplete || !content
	record.MessageMembershipComplete = record.MessageMembershipComplete && content

	certificate, certified, err := loadHistoricalTurnCertificate(txn, source.Scope, source.SourceID)
	if err != nil {
		return err
	}

	if certified {
		if !privacyContainsID(certificate.MessageIDs, message.MessageID) {
			record.Incomplete = true
			record.MessageMembershipComplete = false
		}
	} else if len(record.MessageIDs) < maxCanonicalTraceSourceMessages {
		record.MessageIDs = append(record.MessageIDs, message.MessageID)
	} else {
		record.Incomplete = true
		record.MessageMembershipComplete = false
	}

	return writeSourcePrivacy(txn, record)
}

func (s *CanonicalStore) completeHistoricalSourcePrivacy(
	txn *badger.Txn,
	_ []byte,
	scope memory.Scope,
	id string,
) error {
	source, _, err := loadHistoricalPrivacySource(txn, scope, id)
	if err != nil {
		return err
	}

	record, found, err := loadHistoricalSourcePrivacy(txn, source)
	if err != nil {
		return err
	}

	if !found {
		return invalidSourcePrivacy()
	}

	incomplete, err := privacyScopeIncomplete(txn, scope)
	if err != nil {
		return err
	}

	membership, err := historicalCertifiedMembershipComplete(txn, source)
	if err != nil {
		return err
	}

	record = completePrivacyRecord(record, incomplete, membership)

	return writeSourcePrivacy(txn, record)
}

func completePrivacyRecord(record sourcePrivacy, incomplete, membership bool) sourcePrivacy {
	if record.State != sourcePrivacySensitive {
		record.State = sourcePrivacyUnknown
		if record.HasClassification && !record.Incomplete && !record.StickyUnknown && !incomplete && membership {
			record.State = sourcePrivacyStandard
		}
	}

	record.StickyUnknown = record.StickyUnknown || record.State == sourcePrivacyUnknown
	record.MessageMembershipComplete = record.MessageMembershipComplete && len(record.MessageIDs) > 0
	record.Preparing = false
	record.HasClassification = false
	record.Incomplete = false

	return record
}

func historicalCertifiedMembershipComplete(txn *badger.Txn, source memory.CanonicalSourceRecord) (bool, error) {
	certificate, found, err := loadHistoricalTurnCertificate(txn, source.Scope, source.SourceID)
	if err != nil || !found {
		return true, err
	}

	for _, id := range certificate.MessageIDs {
		key, err := canonicalKey(source.Scope, badgerRecordMessage, id)
		if err != nil {
			return false, err
		}

		var message memory.MessageRecord

		err = getPrivacyPreparationRecord(txn, key, badgerRecordMessage, &message)
		if privacyMissing(err) {
			return false, nil
		}

		if err != nil {
			return false, err
		}

		if message.MessageID != id || message.SourceID != source.SourceID || message.Validate() != nil {
			return false, invalidSourcePrivacy()
		}
	}

	return true, nil
}

func writeSourcePrivacy(txn *badger.Txn, record sourcePrivacy) error {
	key, err := canonicalKey(record.Scope, badgerRecordSourcePrivacy, record.SourceID)
	if err != nil {
		return err
	}

	return putCanonicalRecord(txn, key, badgerRecordSourcePrivacy, record)
}

func privacyContainsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}

	return false
}

func initialHistoricalPrivacyRecord(source memory.CanonicalSourceRecord, previous sourcePrivacy,
	found, valid bool) sourcePrivacy {
	record := sourcePrivacy{Version: sourcePrivacyVersion, CoverageVersion: sourcePrivacyVersion,
		Scope: source.Scope, SourceID: source.SourceID, Payload: source.Payload,
		State: sourcePrivacyUnknown, Incomplete: !valid, Preparing: true, MessageMembershipComplete: true}
	if found {
		record.StickyUnknown = previous.StickyUnknown || previous.State == sourcePrivacyUnknown && !previous.Preparing
	}

	if source.Sensitivity == memory.SensitivitySensitive || found && previous.State == sourcePrivacySensitive {
		record.State = sourcePrivacySensitive
	}

	return record
}
