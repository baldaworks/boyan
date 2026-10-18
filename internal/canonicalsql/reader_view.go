package canonicalsql

import (
	"context"
	"encoding/json"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func loadReaderRecord(ctx context.Context, tx *Tx, resolver *boundaryBoundsResolver,
	id string) (memory.RecallRecord, bool, error) {
	scope := resolver.scope

	view, found, err := loadReaderView(ctx, tx, scope, id)
	if err != nil || !found {
		return memory.RecallRecord{}, false, err
	}

	payload, err := readerPayload(ctx, tx, view.revision.Payload)
	if privacyMissing(err) {
		return memory.RecallRecord{}, false, nil
	}

	if err != nil {
		return memory.RecallRecord{}, false, err
	}

	ref := memory.RevisionRef{ItemID: view.item.ItemID, RevisionID: id}

	bounds, err := resolver.resolve(ref)
	if err != nil {
		return memory.RecallRecord{}, false, err
	}

	if bounds.Sensitivity != memory.SensitivityStandard {
		return memory.RecallRecord{}, false, nil
	}

	view.revision.Temporal = cloneBoundaryTemporal(bounds.Temporal)
	view.revision.Sensitivity = bounds.Sensitivity
	view.revision.Retention = bounds.Retention

	record := buildRecallRecord(scope, view, payload)
	if err := populateRecallRecordSources(scope, view, &record); err != nil {
		return memory.RecallRecord{}, false, err
	}

	return record, true, nil
}

func loadReaderView(ctx context.Context, tx *Tx, scope memory.Scope, id string) (canonicalRecallView, bool, error) {
	view := canonicalRecallView{state: memory.RevisionStateActive}

	revision, err := loadProofRevision(ctx, tx, scope, id)
	if privacyMissing(err) {
		return view, false, nil
	}

	if err != nil {
		return view, false, err
	}

	if revision.RevisionID != id {
		return view, false, corruptRow()
	}

	if revision.Sensitivity != memory.SensitivityStandard {
		return view, false, nil
	}

	view.revision = revision

	item, err := loadProofItem(ctx, tx, scope, revision.ItemID)
	if privacyMissing(err) {
		return view, false, nil
	}

	if err != nil {
		return view, false, err
	}

	view.item = item

	return completeReaderView(ctx, tx, scope, view)
}

func completeReaderView(ctx context.Context, tx *Tx, scope memory.Scope,
	view canonicalRecallView) (canonicalRecallView, bool, error) {
	item, id := view.item, view.revision.RevisionID

	key, err := privacyKey(scope, Head, item.ItemID)
	if err != nil {
		return view, false, err
	}

	var head memory.ItemHead
	if err := getPrivacyRecord(ctx, tx, key, Head, &head); err != nil {
		if privacyMissing(err) {
			return view, false, nil
		}

		return view, false, err
	}

	if head.Validate() != nil || head.ItemID != item.ItemID {
		return view, false, corruptRow()
	}

	if head.RevisionID != id {
		return view, false, nil
	}

	denied, err := privacyDenied(ctx, tx, scope, DeniedRevision, id)
	if err != nil || denied {
		return view, false, err
	}

	found, err := loadReaderSources(ctx, tx, scope, &view)

	return view, found, err
}

func loadReaderSources(ctx context.Context, tx *Tx, scope memory.Scope, view *canonicalRecallView) (bool, error) {
	seen := make(map[string]bool)
	for _, evidence := range view.revision.Evidence {
		if seen[evidence.SourceID] {
			continue
		}

		seen[evidence.SourceID] = true

		source, found, err := readerSource(ctx, tx, scope, evidence.SourceID)
		if err != nil || !found {
			return false, err
		}

		payload, err := readerPayload(ctx, tx, source.Payload)
		if privacyMissing(err) {
			return false, nil
		}

		if err != nil {
			return false, err
		}

		ref, conversation, ok := canonicalSourceRef(payload, scope, evidence.SourceID)
		if ok {
			view.sourceRefs = append(view.sourceRefs, ref)
			view.sourceIDs = append(view.sourceIDs, evidence.SourceID)
			view.conversations = appendUnique(view.conversations, conversation)
		}
	}

	return true, nil
}

func readerSource(ctx context.Context, tx *Tx, scope memory.Scope, id string) (memory.CanonicalSourceRecord,
	bool, error) {
	denied, err := privacyDenied(ctx, tx, scope, DeniedSource, id)
	if err != nil || denied {
		return memory.CanonicalSourceRecord{}, false, err
	}

	source, found, err := loadPrivacySource(ctx, tx, scope, id)
	if err != nil || !found {
		return source, false, err
	}

	found, err = readerSourceAuthorized(ctx, tx, source)

	return source, found, err
}

func readerSourceAuthorized(ctx context.Context, tx *Tx, source memory.CanonicalSourceRecord) (bool, error) {
	record, found, err := loadSourcePrivacy(ctx, tx, source)
	if err != nil || !found {
		return false, err
	}

	if source.Sensitivity != memory.SensitivityStandard || record.State != sourcePrivacyStandard || record.Preparing {
		return false, nil
	}

	if err := validatePrivacyAggregateBindings(ctx, tx, record); err != nil {
		return false, err
	}

	if err := validateReaderCertificate(ctx, tx, source); err != nil {
		return false, err
	}

	return true, nil
}

func validateReaderCertificate(ctx context.Context, tx *Tx, source memory.CanonicalSourceRecord) error {
	certificate, found, err := loadTurnCertificate(ctx, tx, source.Scope, source.SourceID)
	if err != nil || !found {
		return err
	}

	if err := validatePrivacyCertificateSource(ctx, tx, source, certificate); err != nil {
		return err
	}

	for _, id := range certificate.MessageIDs {
		if err := validatePrivacyMemberBinding(ctx, tx, source, id); err != nil {
			return err
		}
	}

	return nil
}

func readerPayload(ctx context.Context, tx *Tx, ref memory.PayloadRef) ([]byte, error) {
	if ref.ByteSize > MaxEnvelopeBytes {
		return nil, limitError()
	}

	key, err := payloadRecordKey(ref)
	if err != nil {
		return nil, err
	}

	var payload []byte
	if err := getPrivacyRecord(ctx, tx, key, Payload, &payload); err != nil {
		return nil, err
	}

	if (memory.CanonicalPayload{Ref: ref, Data: payload}).Validate() != nil {
		return nil, corruptRow()
	}

	return payload, nil
}

func canonicalSourceRef(payload []byte, scope memory.Scope, sourceID string) (memory.SourceRef, string, bool) {
	var source memory.SourceRecord
	if err := json.Unmarshal(payload, &source); err == nil && source.Ref.Scope == scope &&
		source.Ref.ConversationID != "" {
		return source.Ref, source.Ref.ConversationID, true
	}

	var turn canonicalTurnSourcePayload
	if err := json.Unmarshal(payload, &turn); err != nil || turn.ExportID == "" || turn.ConversationID == "" ||
		turn.SourceTurnID == "" {
		return memory.SourceRef{}, "", false
	}

	ref := memory.SourceRef{Scope: scope, ExportID: turn.ExportID, ConversationID: turn.ConversationID,
		SourceTurnID: turn.SourceTurnID}
	if ref.Validate() != nil || sourceID == "" {
		return memory.SourceRef{}, "", false
	}

	return ref, turn.ConversationID, true
}
