package canonicalsql

import (
	"context"
	"encoding/json"
	"slices"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func validateStoredPrivacyBindings(ctx context.Context,
	txn *Tx,
	components []string,
	envelope memory.RecordEnvelope) error {
	switch envelope.RecordType {
	case string(SourcePrivacy):
		record := decodePrivacyAggregate(envelope)
		if record.Preparing || !record.MessageMembershipComplete {
			// Partial captures cannot authorize raw content. Preparation rebuilds
			// them; incomplete content must remain unknown, not prevent retry.
			source, found, err := loadHistoricalPrivacySource(ctx, txn, record.Scope, record.SourceID)
			if err != nil {
				return err
			}

			if !found || !sourcePrivacyBindingsMatch(record, source) {
				return invalidSourcePrivacy()
			}

			return nil
		}

		return validatePrivacyAggregateBindings(ctx, txn, record)
	case string(Message):
		return validatePrivacyMessageCapture(ctx, txn, components, envelope)
	case string(TurnCertificate):
		return validatePrivacyCertificateBindings(ctx, txn, components, envelope)
	default:
		return nil
	}
}

func validatePrivacyAggregateBindings(ctx context.Context, txn *Tx, record sourcePrivacy) error {
	if privacySourceDenied(ctx, txn, record.Scope, record.SourceID) {
		return nil
	}

	source, found, err := loadHistoricalPrivacySource(ctx, txn, record.Scope, record.SourceID)
	if err != nil {
		return err
	}

	if !found || !sourcePrivacyBindingsMatch(record, source) {
		return invalidSourcePrivacy()
	}

	if err := validateCompletePrivacyCapture(ctx, txn, source, record); err != nil {
		return err
	}

	for _, id := range record.MessageIDs {
		if err := validatePrivacyMemberBinding(ctx, txn, source, id); err != nil {
			return err
		}
	}

	return nil
}

func validatePrivacyMemberBinding(ctx context.Context, txn *Tx, source memory.CanonicalSourceRecord, id string) error {
	key, err := privacyKey(source.Scope, Message, id)
	if err != nil {
		return err
	}

	var message memory.MessageRecord

	err = getPrivacyRecord(ctx, txn, key, Message, &message)
	if privacyMissing(err) {
		return validatePrivacyEmbeddedMember(ctx, txn, source, id)
	}

	if err != nil {
		return err
	}

	if message.Validate() != nil || message.SourceID != source.SourceID || message.MessageID != id {
		return invalidSourcePrivacy()
	}

	complete, err := historicalMessageContentComplete(ctx, txn, source, message)
	if err != nil {
		return err
	}

	if !complete {
		return invalidSourcePrivacy()
	}

	return nil
}

func validatePrivacyEmbeddedMember(ctx context.Context, txn *Tx, source memory.CanonicalSourceRecord, id string) error {
	data, err := readPrivacySourcePayload(ctx, txn, source)
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

func validatePrivacyMessageCapture(ctx context.Context,
	txn *Tx,
	components []string,
	envelope memory.RecordEnvelope) error {
	if len(components) != canonicalRecordComponents {
		return invalidSourcePrivacy()
	}

	var message memory.MessageRecord
	if json.Unmarshal(envelope.Payload, &message) != nil ||
		message.Validate() != nil || message.MessageID != components[3] {
		return invalidSourcePrivacy()
	}

	scope := memory.Scope{Key: components[1], Kind: memory.ScopeKind(components[2])}

	source, found, err := loadHistoricalPrivacySource(ctx, txn, scope, message.SourceID)
	if err != nil || !found {
		return err
	}

	if err := validatePrivacyCertifiedMember(ctx, txn, source, message.MessageID); err != nil {
		return err
	}

	return validatePrivacyCapturedMember(ctx, txn, source, message.MessageID)
}

func validatePrivacyCertifiedMember(ctx context.Context,
	txn *Tx,
	source memory.CanonicalSourceRecord,
	id string) error {
	certificate, found, err := loadTurnCertificate(ctx, txn, source.Scope, source.SourceID)
	if err != nil {
		return err
	}

	if found && !slices.Contains(certificate.MessageIDs, id) {
		return invalidSourcePrivacy()
	}

	return nil
}

func validatePrivacyCapturedMember(ctx context.Context, txn *Tx, source memory.CanonicalSourceRecord, id string) error {
	record, found, err := loadSourcePrivacy(ctx, txn, source)
	if err != nil {
		return err
	}

	if found && !record.Preparing && record.MessageMembershipComplete && !slices.Contains(record.MessageIDs, id) {
		return invalidSourcePrivacy()
	}

	return nil
}

func validatePrivacyCertificateBindings(ctx context.Context,
	txn *Tx,
	components []string,
	envelope memory.RecordEnvelope) error {
	if len(components) != canonicalRecordComponents {
		return invalidSourcePrivacy()
	}

	certificate, err := decodePrivacyCertificate(envelope.Payload, components[3])
	if err != nil {
		return err
	}

	scope := memory.Scope{Key: components[1], Kind: memory.ScopeKind(components[2])}
	if privacySourceDenied(ctx, txn, scope, certificate.SourceID) {
		return nil
	}

	source, found, err := loadHistoricalPrivacySource(ctx, txn, scope, certificate.SourceID)
	if err != nil {
		return err
	}

	if !found {
		return invalidSourcePrivacy()
	}

	if err := validatePrivacyCertificateSource(ctx, txn, source, certificate); err != nil {
		return err
	}

	for _, id := range certificate.MessageIDs {
		if err := validatePrivacyMemberBinding(ctx, txn, source, id); err != nil {
			return err
		}
	}

	return nil
}

func validatePrivacyCertificateSource(
	ctx context.Context, txn *Tx, source memory.CanonicalSourceRecord, certificate memory.CanonicalTurnCertificate,
) error {
	data, err := readPrivacySourcePayload(ctx, txn, source)
	if err != nil {
		return err
	}

	native, err := decodeCanonicalTraceTurnPayload(data)
	if err != nil || source.SourceID != memory.TurnSourceID(native.ExportID) || certificate.SourceID != source.SourceID {
		return invalidSourcePrivacy()
	}

	return nil
}

func validateCompletePrivacyCapture(ctx context.Context,
	txn *Tx,
	source memory.CanonicalSourceRecord,
	record sourcePrivacy) error {
	if record.Preparing || !record.MessageMembershipComplete {
		return nil
	}

	certificate, found, err := loadTurnCertificate(ctx, txn, source.Scope, source.SourceID)
	if err != nil {
		return err
	}

	if found {
		if !samePrivacyMemberIDs(record.MessageIDs, certificate.MessageIDs) {
			return invalidSourcePrivacy()
		}

		return nil
	}

	return validateCompleteEmbeddedCapture(ctx, txn, source, record.MessageIDs)
}

func validateCompleteEmbeddedCapture(
	ctx context.Context, txn *Tx, source memory.CanonicalSourceRecord, captured []string,
) error {
	data, err := readPrivacySourcePayload(ctx, txn, source)
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
