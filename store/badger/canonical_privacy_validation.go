package badger

import (
	"encoding/json"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func isCanonicalPrivacyRecord(kind string) bool {
	return kind == badgerRecordSourcePrivacy || kind == badgerRecordSourcePrivacyReady ||
		kind == badgerRecordSourcePrivacyScope
}

func validateCanonicalPrivacyRecord(components []string, envelope memory.RecordEnvelope) error {
	if len(components) == 0 || !isCanonicalPrivacyRecord(components[0]) {
		return nil
	}

	if envelope.RecordType != components[0] || len(envelope.Payload) > maxCanonicalLogicalLineBytes {
		return invalidSourcePrivacy()
	}

	if components[0] == badgerRecordSourcePrivacyReady {
		return validatePrivacyReadyPayload(components, envelope.Payload)
	}

	if len(components) != canonicalRecordComponents {
		return invalidSourcePrivacy()
	}

	scope := memory.Scope{Key: components[1], Kind: memory.ScopeKind(components[2])}
	if scope.Validate() != nil {
		return invalidSourcePrivacy()
	}

	if components[0] == badgerRecordSourcePrivacyScope {
		return validatePrivacyScopePayload(components, scope, envelope.Payload)
	}

	return validatePrivacyAggregatePayload(components, scope, envelope.Payload)
}

func validatePrivacyReadyPayload(components []string, payload []byte) error {
	var record sourcePrivacyReadiness
	if len(components) != 1 || len(payload) > 128 || decodeCanonicalProofJSON(payload, &record) != nil {
		return invalidSourcePrivacy()
	}

	if record.Version != sourcePrivacyVersion || record.CoverageVersion != sourcePrivacyVersion {
		return invalidSourcePrivacy()
	}

	return nil
}

func validatePrivacyScopePayload(components []string, scope memory.Scope, payload []byte) error {
	var record sourcePrivacyScope
	if decodeCanonicalProofJSON(payload, &record) != nil {
		return invalidSourcePrivacy()
	}

	if components[3] != "coverage" || record.Version != sourcePrivacyVersion || record.Scope != scope {
		return invalidSourcePrivacy()
	}

	return nil
}

func validatePrivacyAggregatePayload(components []string, scope memory.Scope, payload []byte) error {
	var record sourcePrivacy
	if decodeCanonicalProofJSON(payload, &record) != nil {
		return invalidSourcePrivacy()
	}

	if record.Version != sourcePrivacyVersion || record.CoverageVersion != sourcePrivacyVersion ||
		record.Scope != scope || record.SourceID != components[3] || record.Payload.Validate() != nil {
		return invalidSourcePrivacy()
	}

	if !sourcePrivacyStateValid(record) {
		return invalidSourcePrivacy()
	}

	return nil
}

func decodePrivacyAggregate(envelope memory.RecordEnvelope) sourcePrivacy {
	var record sourcePrivacy

	_ = json.Unmarshal(envelope.Payload, &record)

	return record
}
