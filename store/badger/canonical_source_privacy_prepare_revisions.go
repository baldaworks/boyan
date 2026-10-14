package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) collectHistoricalPrivacyRevision(
	txn *badger.Txn,
	key []byte,
	scope memory.Scope,
	id string,
) error {
	var revision memory.MemoryRevision
	if err := getPrivacyPreparationRecord(txn, key, badgerRecordRevision, &revision); err != nil {
		return err
	}

	if !historicalRevisionIdentityValid(revision, id) {
		return invalidSourcePrivacy()
	}

	supported, err := historicalRevisionSupported(txn, scope, revision)
	if err != nil {
		return err
	}

	refs, complete, err := historicalPrivacyRevisionEvidence(txn, scope, revision, supported)
	if err != nil {
		return err
	}

	if len(refs) == 0 {
		return putPrivacyScope(txn, scope, true)
	}

	for _, ref := range refs {
		if err := mergeHistoricalPrivacyEvidence(txn, scope, ref, revision, supported && complete); err != nil {
			return err
		}
	}

	if supported {
		return putCanonicalMutationEvidenceEdges(txn, scope, revision)
	}

	return nil
}

func historicalPrivacyRevisionEvidence(txn *badger.Txn, scope memory.Scope, revision memory.MemoryRevision,
	supported bool) ([]memory.EvidenceRef, bool, error) {
	refs := historicalDirectPrivacyRefs(revision)
	if refs == nil {
		return nil, false, nil
	}

	complete := supported

	for _, id := range revision.Parents {
		valid, err := appendHistoricalPrivacyParent(txn, scope, id, refs)
		if err != nil {
			return nil, false, err
		}

		complete = complete && valid

		if len(refs) > memory.MaxSourcesPerRevision {
			return nil, false, nil
		}
	}

	result, valid, err := validateHistoricalPrivacyRefs(txn, scope, refs)

	return result, complete && valid, err
}

func historicalDirectPrivacyRefs(revision memory.MemoryRevision) map[memory.EvidenceRef]struct{} {
	if len(revision.Evidence) > memory.MaxSourcesPerRevision || len(revision.Parents) > memory.MaxSourcesPerRevision {
		return nil
	}

	refs := make(map[memory.EvidenceRef]struct{}, len(revision.Evidence))
	for _, ref := range revision.Evidence {
		if ref.Validate() != nil {
			return nil
		}

		refs[ref] = struct{}{}
	}

	return refs
}

func appendHistoricalPrivacyParent(txn *badger.Txn, scope memory.Scope, id string,
	refs map[memory.EvidenceRef]struct{}) (bool, error) {
	key, err := canonicalKey(scope, badgerRecordRevision, id)
	if err != nil {
		return false, err
	}

	var parent memory.MemoryRevision

	err = getPrivacyPreparationRecord(txn, key, badgerRecordRevision, &parent)
	if privacyMissing(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if parent.RevisionID != "" && parent.RevisionID != id {
		return false, invalidSourcePrivacy()
	}

	if parent.Validate() != nil {
		return false, nil
	}

	for _, ref := range parent.Evidence {
		refs[ref] = struct{}{}
	}

	return true, nil
}

func validateHistoricalPrivacyRefs(txn *badger.Txn, scope memory.Scope,
	refs map[memory.EvidenceRef]struct{}) ([]memory.EvidenceRef, bool, error) {
	result := make([]memory.EvidenceRef, 0, len(refs))
	complete := true

	for ref := range refs {
		valid, err := validateHistoricalPrivacyEvidence(txn, scope, ref)
		if err != nil {
			return nil, false, err
		}

		complete = complete && valid

		result = append(result, ref)
	}

	return result, complete, nil
}

func validateHistoricalPrivacyEvidence(txn *badger.Txn, scope memory.Scope, ref memory.EvidenceRef) (bool, error) {
	source, found, err := loadHistoricalPrivacySource(txn, scope, ref.SourceID)
	if err != nil || !found {
		return false, err
	}

	representable, err := historicalSourceRepresentable(txn, source)
	if err != nil || !representable {
		return false, err
	}

	key, err := canonicalKey(scope, badgerRecordMessage, ref.MessageID)
	if err != nil {
		return false, err
	}

	var message memory.MessageRecord

	err = getPrivacyPreparationRecord(txn, key, badgerRecordMessage, &message)
	if privacyMissing(err) {
		return validateHistoricalEmbeddedEvidence(txn, source, ref)
	}

	if err != nil {
		return false, err
	}

	valid, err := validatePrivacyMessage(message, ref)
	if err != nil || !valid {
		return false, err
	}

	return validatePrivacyRawEvidence(txn, source, ref)
}

func mergeHistoricalPrivacyEvidence(txn *badger.Txn, scope memory.Scope, ref memory.EvidenceRef,
	revision memory.MemoryRevision, complete bool) error {
	source, found, err := loadHistoricalPrivacySource(txn, scope, ref.SourceID)
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

	record.HasClassification = true
	if !complete {
		record.Incomplete = true
	}

	if revision.Sensitivity == memory.SensitivitySensitive && revision.Validate() == nil {
		record.State = sourcePrivacySensitive
	}

	return writeSourcePrivacy(txn, record)
}

func validateHistoricalEmbeddedEvidence(
	txn *badger.Txn,
	source memory.CanonicalSourceRecord,
	ref memory.EvidenceRef,
) (bool, error) {
	payload, err := readPrivacySourcePayload(txn, source)
	if err != nil {
		return false, err
	}

	_, embedded, err := migratedTraceSourceRecord(payload, source.Scope)
	if !embedded {
		return false, nil
	}

	if err != nil {
		return false, invalidSourcePrivacy()
	}

	return validatePrivacyRawEvidence(txn, source, ref)
}

func historicalRevisionIdentityValid(revision memory.MemoryRevision, id string) bool {
	if revision.RevisionID != "" {
		return revision.RevisionID == id
	}

	return revision.SchemaVersion != memory.MemorySchemaVersion
}

func historicalRevisionSupported(txn *badger.Txn, scope memory.Scope, revision memory.MemoryRevision) (bool, error) {
	if revision.Validate() != nil {
		return false, nil
	}

	return historicalRevisionItemComplete(txn, scope, revision)
}
