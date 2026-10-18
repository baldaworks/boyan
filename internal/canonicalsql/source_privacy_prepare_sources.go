package canonicalsql

import (
	"context"
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func loadHistoricalPrivacySource(
	ctx context.Context, txn *Tx,
	scope memory.Scope,
	id string,
) (memory.CanonicalSourceRecord, bool, error) {
	key, err := privacyKey(scope, Source, id)
	if err != nil {
		return memory.CanonicalSourceRecord{}, false, err
	}

	var source memory.CanonicalSourceRecord

	err = getPrivacyRecord(ctx, txn, key, Source, &source)
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

func initializeHistoricalSourcePrivacy(
	ctx context.Context, txn *Tx,
	_ Key,
	scope memory.Scope,
	id string,
) error {
	source, _, err := loadHistoricalPrivacySource(ctx, txn, scope, id)
	if err != nil {
		return err
	}

	previous, found, err := loadSourcePrivacy(ctx, txn, source)
	if err != nil {
		return err
	}

	valid, err := historicalSourceRepresentable(ctx, txn, source)
	if err != nil {
		return err
	}

	record := initialHistoricalPrivacyRecord(source, previous, found, valid)
	if err := writeSourcePrivacy(ctx, txn, record); err != nil {
		return err
	}

	return putPrivacyScope(ctx, txn, scope, false)
}

func historicalSourceRepresentable(ctx context.Context, txn *Tx, source memory.CanonicalSourceRecord) (bool, error) {
	payload, err := readPrivacySourcePayload(ctx, txn, source)
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

func collectHistoricalPrivacyMessage(
	ctx context.Context, txn *Tx,
	key Key,
	scope memory.Scope,
	id string,
) error {
	var message memory.MessageRecord
	if err := getPrivacyRecord(ctx, txn, key, Message, &message); err != nil {
		return err
	}

	if message.MessageID != id || message.Validate() != nil {
		return invalidSourcePrivacy()
	}

	source, found, err := loadHistoricalPrivacySource(ctx, txn, scope, message.SourceID)
	if err != nil {
		return err
	}

	if !found {
		return putPrivacyScope(ctx, txn, scope, true)
	}

	record, exists, err := loadSourcePrivacy(ctx, txn, source)
	if err != nil {
		return err
	}

	if !exists {
		return invalidSourcePrivacy()
	}

	return mergeHistoricalMessageMembership(ctx, txn, source, record, message)
}

func mergeHistoricalMessageMembership(ctx context.Context, txn *Tx, source memory.CanonicalSourceRecord,
	record sourcePrivacy, message memory.MessageRecord) error {
	content, err := historicalMessageContentComplete(ctx, txn, source, message)
	if err != nil {
		return err
	}

	record.Incomplete = record.Incomplete || !content
	record.MessageMembershipComplete = record.MessageMembershipComplete && content

	certificate, certified, err := loadTurnCertificate(ctx, txn, source.Scope, source.SourceID)
	if err != nil {
		return err
	}

	if certified {
		if !privacyContainsID(certificate.MessageIDs, message.MessageID) {
			record.Incomplete = true
			record.MessageMembershipComplete = false
		}
	} else if len(record.MessageIDs) < sourcePrivacyBatchSize {
		record.MessageIDs = append(record.MessageIDs, message.MessageID)
	} else {
		record.Incomplete = true
		record.MessageMembershipComplete = false
	}

	return writeSourcePrivacy(ctx, txn, record)
}

func completeHistoricalSourcePrivacy(
	ctx context.Context, txn *Tx,
	_ Key,
	scope memory.Scope,
	id string,
) error {
	source, _, err := loadHistoricalPrivacySource(ctx, txn, scope, id)
	if err != nil {
		return err
	}

	record, found, err := loadSourcePrivacy(ctx, txn, source)
	if err != nil {
		return err
	}

	if !found {
		return invalidSourcePrivacy()
	}

	incomplete, err := privacyScopeIncomplete(ctx, txn, scope)
	if err != nil {
		return err
	}

	membership, err := historicalCertifiedMembershipComplete(ctx, txn, source)
	if err != nil {
		return err
	}

	record = completePrivacyRecord(record, incomplete, membership)

	return writeSourcePrivacy(ctx, txn, record)
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

func historicalCertifiedMembershipComplete(ctx context.Context,
	txn *Tx,
	source memory.CanonicalSourceRecord) (bool,
	error) {
	certificate, found, err := loadTurnCertificate(ctx, txn, source.Scope, source.SourceID)
	if err != nil || !found {
		return true, err
	}

	for _, id := range certificate.MessageIDs {
		key, err := privacyKey(source.Scope, Message, id)
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

		if message.MessageID != id || message.SourceID != source.SourceID || message.Validate() != nil {
			return false, invalidSourcePrivacy()
		}
	}

	return true, nil
}

func writeSourcePrivacy(ctx context.Context, txn *Tx, record sourcePrivacy) error {
	key, err := privacyKey(record.Scope, SourcePrivacy, record.SourceID)
	if err != nil {
		return err
	}

	return putPrivacyRecord(ctx, txn, key, SourcePrivacy, record)
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
