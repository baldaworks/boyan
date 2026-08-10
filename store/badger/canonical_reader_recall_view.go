package badger

import (
	"context"
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (r *CanonicalReader) loadCanonicalView(ctx context.Context, scope memory.Scope, revisionID string, activeOnly bool) (canonicalRecallView, bool, error) {
	if err := scope.Validate(); err != nil {
		return canonicalRecallView{}, false, err
	}
	if strings.TrimSpace(revisionID) == "" {
		return canonicalRecallView{}, false, memory.PermanentError(memory.CodeInvalidDerived, "canonical revision id is required", nil)
	}
	if r == nil || r.store == nil || r.store.db == nil {
		return canonicalRecallView{}, false, memory.PermanentError(memory.CodeStoreFailure, "canonical reader store is closed", nil)
	}
	r.store.maintenanceMu.RLock()
	defer r.store.maintenanceMu.RUnlock()

	view := canonicalRecallView{state: memory.RevisionStateActive}
	err := r.store.db.View(func(txn *badger.Txn) error {
		revisionKey, err := canonicalKey(scope, badgerRecordRevision, revisionID)
		if err != nil {
			return err
		}
		if err := getCanonicalRecord(txn, revisionKey, badgerRecordRevision, &view.revision); err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return err
			}
			return err
		}
		if err := view.revision.Validate(); err != nil {
			return memory.PermanentError(memory.CodeStoreFailure, "stored canonical revision is invalid", err)
		}
		itemKey, err := canonicalKey(scope, badgerRecordItem, view.revision.ItemID)
		if err != nil {
			return err
		}
		if err := getCanonicalRecord(txn, itemKey, badgerRecordItem, &view.item); err != nil {
			return err
		}
		if err := view.item.Validate(); err != nil || view.item.Scope != scope || view.item.ItemID != view.revision.ItemID {
			return memory.PermanentError(memory.CodeStoreFailure, "stored canonical item is invalid", err)
		}
		headKey, keyErr := canonicalKey(scope, badgerRecordHead, view.item.ItemID)
		if keyErr != nil {
			return keyErr
		}
		var head memory.ItemHead
		if err := getCanonicalRecord(txn, headKey, badgerRecordHead, &head); err != nil {
			return err
		}
		if err := head.Validate(); err != nil || head.ItemID != view.item.ItemID {
			return memory.PermanentError(memory.CodeStoreFailure, "stored canonical item head is invalid", err)
		}
		if head.RevisionID != view.revision.RevisionID {
			view.state = memory.RevisionStateSuperseded
			if activeOnly {
				return errCanonicalInactive
			}
		}
		if err := checkDeniedRevision(txn, scope, view.revision.RevisionID); err != nil {
			return err
		}
		return loadCanonicalViewSources(txn, scope, &view)
	})
	if errors.Is(err, badger.ErrKeyNotFound) || errors.Is(err, errCanonicalInactive) || errors.Is(err, errCanonicalDenied) {
		return canonicalRecallView{}, false, nil
	}
	if err != nil {
		return canonicalRecallView{}, false, canonicalStoreError("load canonical recall view", err)
	}
	return view, true, nil
}

func checkDeniedRevision(txn *badger.Txn, scope memory.Scope, revisionID string) error {
	deniedKey, err := canonicalKey(scope, badgerRecordDeniedRevision, revisionID)
	if err != nil {
		return err
	}
	var denied badgerDeniedRevision
	if err := getCanonicalRecord(txn, deniedKey, badgerRecordDeniedRevision, &denied); err == nil {
		return errCanonicalDenied
	} else if !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	return nil
}

func loadCanonicalViewSources(txn *badger.Txn, scope memory.Scope, view *canonicalRecallView) error {
	seenSources := make(map[string]struct{}, len(view.revision.Evidence))
	for _, evidence := range view.revision.Evidence {
		if _, exists := seenSources[evidence.SourceID]; exists {
			continue
		}
		seenSources[evidence.SourceID] = struct{}{}
		if err := checkDeniedSource(txn, scope, evidence.SourceID); err != nil {
			return err
		}
		source, err := loadCanonicalSourceRecord(txn, scope, evidence.SourceID)
		if err != nil {
			return err
		}
		payload, err := loadCanonicalSourcePayload(txn, source.Payload)
		if err != nil {
			return err
		}
		ref, sessionID, ok := canonicalSourceRef(payload, scope, evidence.SourceID)
		if ok {
			view.sourceRefs = append(view.sourceRefs, ref)
			view.sourceIDs = append(view.sourceIDs, evidence.SourceID)
			if sessionID != "" {
				view.sessions = appendUnique(view.sessions, sessionID)
			}
		}
	}
	return nil
}

func checkDeniedSource(txn *badger.Txn, scope memory.Scope, sourceID string) error {
	deniedSourceKey, err := canonicalKey(scope, badgerRecordDeniedSource, sourceID)
	if err != nil {
		return err
	}
	var deniedSource badgerDeniedSource
	if err := getCanonicalRecord(txn, deniedSourceKey, badgerRecordDeniedSource, &deniedSource); err == nil {
		return errCanonicalDenied
	} else if !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	return nil
}

func loadCanonicalSourceRecord(txn *badger.Txn, scope memory.Scope, sourceID string) (memory.CanonicalSourceRecord, error) {
	sourceKey, err := canonicalKey(scope, badgerRecordSource, sourceID)
	if err != nil {
		return memory.CanonicalSourceRecord{}, err
	}
	var source memory.CanonicalSourceRecord
	if err := getCanonicalRecord(txn, sourceKey, badgerRecordSource, &source); err != nil {
		return memory.CanonicalSourceRecord{}, err
	}
	if err := source.Validate(); err != nil || source.Scope != scope {
		return memory.CanonicalSourceRecord{}, memory.PermanentError(memory.CodeStoreFailure, "stored canonical source is invalid", err)
	}
	return source, nil
}

func loadCanonicalSourcePayload(txn *badger.Txn, ref memory.PayloadRef) ([]byte, error) {
	payloadKey, err := canonicalKey(payloadScope(), badgerRecordPayload, ref.ID)
	if err != nil {
		return nil, err
	}
	var payload []byte
	if err := getCanonicalRecord(txn, payloadKey, badgerRecordPayload, &payload); err != nil {
		return nil, err
	}
	if !isBadgerPayloadValid(payload, ref) {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "stored canonical source payload is invalid", nil)
	}
	return payload, nil
}
