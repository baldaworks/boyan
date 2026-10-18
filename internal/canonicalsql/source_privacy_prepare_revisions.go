package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func collectHistoricalPrivacyRevision(
	ctx context.Context, txn *Tx,
	key Key,
	scope memory.Scope,
	id string,
) error {
	var revision memory.MemoryRevision
	if err := getPrivacyRecord(ctx, txn, key, Revision, &revision); err != nil {
		return err
	}

	if !historicalRevisionIdentityValid(revision, id) {
		return invalidSourcePrivacy()
	}

	supported, err := historicalRevisionSupported(ctx, txn, scope, revision)
	if err != nil {
		return err
	}

	refs, complete, err := historicalPrivacyRevisionEvidence(ctx, txn, scope, revision, supported)
	if err != nil {
		return err
	}

	if len(refs) == 0 {
		return putPrivacyScope(ctx, txn, scope, true)
	}

	for _, ref := range refs {
		if err := mergeHistoricalPrivacyEvidence(ctx, txn, scope, ref, revision, supported && complete); err != nil {
			return err
		}
	}

	if supported {
		return putCanonicalMutationEvidenceEdges(ctx, txn, scope, revision)
	}

	return nil
}

func historicalPrivacyRevisionEvidence(ctx context.Context, txn *Tx, scope memory.Scope, revision memory.MemoryRevision,
	supported bool) ([]memory.EvidenceRef, bool, error) {
	refs := historicalDirectPrivacyRefs(revision)
	if refs == nil {
		return nil, false, nil
	}

	complete := supported

	for _, id := range revision.Parents {
		valid, err := appendHistoricalPrivacyParent(ctx, txn, scope, id, refs)
		if err != nil {
			return nil, false, err
		}

		complete = complete && valid

		if len(refs) > memory.MaxSourcesPerRevision {
			return nil, false, nil
		}
	}

	result, valid, err := validateHistoricalPrivacyRefs(ctx, txn, scope, refs)

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

func appendHistoricalPrivacyParent(ctx context.Context, txn *Tx, scope memory.Scope, id string,
	refs map[memory.EvidenceRef]struct{}) (bool, error) {
	key, err := privacyKey(scope, Revision, id)
	if err != nil {
		return false, err
	}

	var parent memory.MemoryRevision

	err = getPrivacyRecord(ctx, txn, key, Revision, &parent)
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

func validateHistoricalPrivacyRefs(ctx context.Context, txn *Tx, scope memory.Scope,
	refs map[memory.EvidenceRef]struct{}) ([]memory.EvidenceRef, bool, error) {
	result := make([]memory.EvidenceRef, 0, len(refs))
	complete := true

	for ref := range refs {
		valid, err := validateHistoricalPrivacyEvidence(ctx, txn, scope, ref)
		if err != nil {
			return nil, false, err
		}

		complete = complete && valid

		result = append(result, ref)
	}

	return result, complete, nil
}

func validateHistoricalPrivacyEvidence(ctx context.Context,
	txn *Tx,
	scope memory.Scope,
	ref memory.EvidenceRef) (bool,
	error) {
	source, found, err := loadHistoricalPrivacySource(ctx, txn, scope, ref.SourceID)
	if err != nil || !found {
		return false, err
	}

	representable, err := historicalSourceRepresentable(ctx, txn, source)
	if err != nil || !representable {
		return false, err
	}

	key, err := privacyKey(scope, Message, ref.MessageID)
	if err != nil {
		return false, err
	}

	var message memory.MessageRecord

	err = getPrivacyRecord(ctx, txn, key, Message, &message)
	if privacyMissing(err) {
		return validateHistoricalEmbeddedEvidence(ctx, txn, source, ref)
	}

	if err != nil {
		return false, err
	}

	valid, err := validatePrivacyMessage(message, ref)
	if err != nil || !valid {
		return false, err
	}

	return validatePrivacyRawEvidence(ctx, txn, source, ref)
}

func mergeHistoricalPrivacyEvidence(ctx context.Context, txn *Tx, scope memory.Scope, ref memory.EvidenceRef,
	revision memory.MemoryRevision, complete bool) error {
	source, found, err := loadHistoricalPrivacySource(ctx, txn, scope, ref.SourceID)
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

	record.HasClassification = true
	if !complete {
		record.Incomplete = true
	}

	if revision.Sensitivity == memory.SensitivitySensitive && revision.Validate() == nil {
		record.State = sourcePrivacySensitive
	}

	return writeSourcePrivacy(ctx, txn, record)
}

func validateHistoricalEmbeddedEvidence(
	ctx context.Context, txn *Tx,
	source memory.CanonicalSourceRecord,
	ref memory.EvidenceRef,
) (bool, error) {
	payload, err := readPrivacySourcePayload(ctx, txn, source)
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

	return validatePrivacyRawEvidence(ctx, txn, source, ref)
}

func historicalRevisionIdentityValid(revision memory.MemoryRevision, id string) bool {
	if revision.RevisionID != "" {
		return revision.RevisionID == id
	}

	return revision.SchemaVersion != memory.MemorySchemaVersion
}

func historicalRevisionSupported(ctx context.Context,
	txn *Tx,
	scope memory.Scope,
	revision memory.MemoryRevision) (bool,
	error) {
	if revision.Validate() != nil {
		return false, nil
	}

	return historicalRevisionItemComplete(ctx, txn, scope, revision)
}
