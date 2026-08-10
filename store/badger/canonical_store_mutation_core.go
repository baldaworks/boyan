package badger

import (
	"context"
	"errors"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type badgerProvenanceEdge struct {
	ChildRevisionID  string `json:"child_revision_id"`
	ParentRevisionID string `json:"parent_revision_id"`
}

// ApplyCanonicalMutation appends immutable records and advances the exact
// scope cursor in one Badger transaction.
func (s *CanonicalStore) ApplyCanonicalMutation(ctx context.Context, mutation memory.CanonicalMutation) (memory.CanonicalMutationOutcome, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}
	if err := mutation.Validate(); err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}
	if s == nil {
		return memory.CanonicalMutationOutcome{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.CanonicalMutationOutcome{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	scopeKey, err := badgerScopeKey(mutation.Scope)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}
	operationKey, err := badgerOperationKey(mutation.Scope, mutation.Operation.OperationID)
	if err != nil {
		return memory.CanonicalMutationOutcome{}, err
	}
	var outcome memory.CanonicalMutationOutcome
	err = s.db.Update(func(txn *badger.Txn) error {
		var operation badgerCanonicalOperation
		err := getCanonicalRecord(txn, operationKey, badgerRecordOperation, &operation)
		if err == nil {
			if operation.Fingerprint != mutation.Operation.Fingerprint {
				return memory.PermanentError(memory.CodeConflict, "canonical operation identity was reused", nil)
			}
			if err := operation.Outcome.Validate(); err != nil {
				return err
			}
			outcome = operation.Outcome
			return nil
		}
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: mutation.Scope}
		err = getCanonicalRecord(txn, scopeKey, badgerRecordScope, &state)
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		if err := state.Validate(); err != nil || state.Scope != mutation.Scope {
			return memory.PermanentError(memory.CodeStoreFailure, "stored canonical scope state is invalid", err)
		}
		if state.Version != mutation.ExpectedScopeVersion {
			return memory.PermanentError(memory.CodeConflict, "canonical scope version changed", nil)
		}
		if err := s.putCanonicalMutationRecords(txn, mutation); err != nil {
			return err
		}
		state.Version++
		state.ChangeSeq++
		outcome = memory.CanonicalMutationOutcome{ScopeVersion: state.Version, ChangeSeq: state.ChangeSeq}
		for _, revision := range mutation.Revisions {
			outcome.RevisionIDs = append(outcome.RevisionIDs, revision.RevisionID)
		}
		if err := outcome.Validate(); err != nil {
			return err
		}
		change := memory.ScopeChange{
			Sequence:    state.ChangeSeq,
			OperationID: mutation.Operation.OperationID,
			OccurredAt:  mutation.Operation.CommittedAt,
			RevisionIDs: outcome.RevisionIDs,
		}
		changeKey, err := badgerScopeChangeKey(mutation.Scope, change.Sequence)
		if err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, changeKey, badgerRecordChange, change); err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, scopeKey, badgerRecordScope, state); err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, operationKey, badgerRecordOperation, badgerCanonicalOperation{Fingerprint: mutation.Operation.Fingerprint, Outcome: outcome}); err != nil {
			return err
		}
		if s.beforeCanonicalMutationCommit != nil {
			return s.beforeCanonicalMutationCommit()
		}
		return nil
	})
	if err != nil {
		return memory.CanonicalMutationOutcome{}, canonicalStoreError("apply canonical mutation", err)
	}
	return outcome, nil
}

func (s *CanonicalStore) putCanonicalMutationRecords(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	if err := validateBadgerMutationReferences(txn, mutation); err != nil {
		return err
	}
	if err := validateBadgerMutationProvenance(txn, mutation); err != nil {
		return err
	}
	for _, payload := range mutation.Payloads {
		key, err := canonicalKey(memory.Scope{Key: "internal:payload", Kind: memory.ScopeKindPersonal}, badgerRecordPayload, payload.Ref.ID)
		if err != nil {
			return err
		}
		if err := putCanonicalImmutableRecord(txn, key, badgerRecordPayload, payload.Data); err != nil {
			return err
		}
	}
	for _, source := range mutation.Sources {
		if err := putBadgerCanonicalRecord(txn, mutation.Scope, badgerRecordSource, source.SourceID, source); err != nil {
			return err
		}
	}
	for _, message := range mutation.Messages {
		if err := putBadgerCanonicalRecord(txn, mutation.Scope, badgerRecordMessage, message.MessageID, message); err != nil {
			return err
		}
	}
	for _, item := range mutation.Items {
		if err := putBadgerCanonicalRecord(txn, mutation.Scope, badgerRecordItem, item.ItemID, item); err != nil {
			return err
		}
	}
	for _, revision := range mutation.Revisions {
		if err := putBadgerCanonicalRecord(txn, mutation.Scope, badgerRecordRevision, revision.RevisionID, revision); err != nil {
			return err
		}
		for _, parentID := range revision.Parents {
			edge := badgerProvenanceEdge{ChildRevisionID: revision.RevisionID, ParentRevisionID: parentID}
			forwardKey, err := badgerProvenanceKey(mutation.Scope, badgerRecordProvenanceForward, revision.RevisionID, parentID)
			if err != nil {
				return err
			}
			if err := putCanonicalImmutableRecord(txn, forwardKey, badgerRecordProvenanceForward, edge); err != nil {
				return err
			}
			reverseKey, err := badgerProvenanceKey(mutation.Scope, badgerRecordProvenanceReverse, parentID, revision.RevisionID)
			if err != nil {
				return err
			}
			if err := putCanonicalImmutableRecord(txn, reverseKey, badgerRecordProvenanceReverse, edge); err != nil {
				return err
			}
		}
		for _, evidence := range revision.Evidence {
			key, err := badgerProvenanceKey(mutation.Scope, badgerRecordSourceRevision, evidence.SourceID, revision.RevisionID)
			if err != nil {
				return err
			}
			edge := badgerProvenanceEdge{ChildRevisionID: revision.RevisionID, ParentRevisionID: evidence.SourceID}
			if err := putCanonicalImmutableRecord(txn, key, badgerRecordSourceRevision, edge); err != nil {
				return err
			}
		}
	}
	for _, lifecycle := range mutation.Lifecycle {
		if err := putBadgerCanonicalRecord(txn, mutation.Scope, badgerRecordLifecycle, lifecycle.EventID, lifecycle); err != nil {
			return err
		}
	}
	for _, head := range mutation.Heads {
		key, err := canonicalKey(mutation.Scope, badgerRecordHead, head.ItemID)
		if err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, key, badgerRecordHead, head); err != nil {
			return err
		}
	}
	for _, delivery := range mutation.Delivery {
		if err := putBadgerCanonicalRecord(txn, mutation.Scope, badgerRecordDelivery, delivery.DeliveryID, delivery); err != nil {
			return err
		}
	}
	return nil
}

func validateBadgerMutationReferences(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	items := make(map[string]struct{}, len(mutation.Items))
	for _, item := range mutation.Items {
		items[item.ItemID] = struct{}{}
	}
	revisions := make(map[string]struct{}, len(mutation.Revisions))
	for _, revision := range mutation.Revisions {
		revisions[revision.RevisionID] = struct{}{}
		if _, exists := items[revision.ItemID]; !exists {
			if err := badgerCanonicalRecordExists(txn, mutation.Scope, badgerRecordItem, revision.ItemID); err != nil {
				return fmt.Errorf("revision item reference: %w", err)
			}
		}
	}
	for _, lifecycle := range mutation.Lifecycle {
		if _, exists := revisions[lifecycle.RevisionID]; !exists {
			if err := badgerCanonicalRecordExists(txn, mutation.Scope, badgerRecordRevision, lifecycle.RevisionID); err != nil {
				return fmt.Errorf("lifecycle revision reference: %w", err)
			}
		}
	}
	for _, head := range mutation.Heads {
		if _, exists := revisions[head.RevisionID]; !exists {
			if err := badgerCanonicalRecordExists(txn, mutation.Scope, badgerRecordRevision, head.RevisionID); err != nil {
				return fmt.Errorf("head revision reference: %w", err)
			}
		}
	}
	return nil
}

func badgerCanonicalRecordExists(txn *badger.Txn, scope memory.Scope, recordType, id string) error {
	key, err := canonicalKey(scope, recordType, id)
	if err != nil {
		return err
	}
	if _, err := txn.Get(key); errors.Is(err, badger.ErrKeyNotFound) {
		return memory.PermanentError(memory.CodeConflict, "canonical record reference does not exist", nil)
	} else if err != nil {
		return err
	}
	return nil
}

func validateBadgerMutationProvenance(txn *badger.Txn, mutation memory.CanonicalMutation) error {
	revisions := make(map[string]memory.MemoryRevision, len(mutation.Revisions))
	for _, revision := range mutation.Revisions {
		revisions[revision.RevisionID] = revision
	}
	for _, revision := range mutation.Revisions {
		for _, parentID := range revision.Parents {
			if _, exists := revisions[parentID]; exists {
				continue
			}
			parentKey, err := canonicalKey(mutation.Scope, badgerRecordRevision, parentID)
			if err != nil {
				return err
			}
			if _, err := txn.Get(parentKey); errors.Is(err, badger.ErrKeyNotFound) {
				return memory.PermanentError(memory.CodeConflict, "canonical provenance parent does not exist", nil)
			} else if err != nil {
				return err
			}
		}
	}
	colors := make(map[string]uint8, len(revisions))
	var visit func(string) error
	visit = func(revisionID string) error {
		switch colors[revisionID] {
		case 1:
			return memory.PermanentError(memory.CodeInvalidDerived, "canonical provenance cycle", nil)
		case 2:
			return nil
		}
		revision, exists := revisions[revisionID]
		if !exists {
			return nil
		}
		colors[revisionID] = 1
		for _, parentID := range revision.Parents {
			if err := visit(parentID); err != nil {
				return err
			}
		}
		colors[revisionID] = 2
		return nil
	}
	for revisionID := range revisions {
		if err := visit(revisionID); err != nil {
			return err
		}
	}
	return nil
}
