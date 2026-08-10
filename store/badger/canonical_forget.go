package badger

import (
	"context"
	"errors"
	"strings"

	"github.com/dgraph-io/badger/v4"
	"github.com/baldaworks/boyan"
)

const maxCanonicalScrubPayloads = 4096

type badgerCanonicalForgetOutcome struct {
	Fingerprint string                      `json:"fingerprint"`
	Outcome     boyan.ForgetOutcome `json:"outcome"`
}

var _ boyan.CanonicalForgetEnumerator = (*CanonicalStore)(nil)
var _ boyan.CanonicalSourceIdentityResolver = (*CanonicalStore)(nil)
var _ boyan.CanonicalForgetOutcomeStore = (*CanonicalStore)(nil)

func (s *CanonicalStore) ListCanonicalSourceRefs(ctx context.Context, scope boyan.Scope, afterSourceID string, limit uint32) ([]boyan.SourceRef, string, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, "", err
	}
	if err := scope.Validate(); err != nil {
		return nil, "", err
	}
	if limit == 0 || limit > 512 || (afterSourceID != "" && strings.TrimSpace(afterSourceID) != afterSourceID) {
		return nil, "", boyan.PermanentError(boyan.CodeLimitExceeded, "canonical source enumeration limit is invalid", nil)
	}
	if s == nil {
		return nil, "", boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return nil, "", boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	prefix, err := canonicalPrefix(scope, badgerRecordSource)
	if err != nil {
		return nil, "", err
	}
	refs := make([]boyan.SourceRef, 0, limit)
	next := ""
	err = s.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix) && uint32(len(refs)) < limit; iterator.Next() {
			var source boyan.SourceRecordV2
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordSource, &source); err != nil {
				return err
			}
			if source.SourceID <= afterSourceID {
				continue
			}
			deniedKey, keyErr := canonicalKey(scope, badgerRecordDeniedSource, source.SourceID)
			if keyErr != nil {
				return keyErr
			}
			var denied badgerDeniedSource
			if deniedErr := getCanonicalRecord(txn, deniedKey, badgerRecordDeniedSource, &denied); deniedErr == nil {
				continue
			} else if !errors.Is(deniedErr, badger.ErrKeyNotFound) {
				return deniedErr
			}
			payloadKey, keyErr := canonicalKey(payloadScope(), badgerRecordPayload, source.Payload.ID)
			if keyErr != nil {
				return keyErr
			}
			var payload []byte
			if err := getCanonicalRecord(txn, payloadKey, badgerRecordPayload, &payload); err != nil {
				return err
			}
			if !isBadgerPayloadValid(payload, source.Payload) {
				return boyan.PermanentError(boyan.CodeStoreFailure, "canonical source payload is invalid", nil)
			}
			ref, _, ok := canonicalSourceRef(payload, scope, source.SourceID)
			if !ok {
				return boyan.PermanentError(boyan.CodeInvalidDerived, "canonical source identity is not representable", nil)
			}
			refs = append(refs, ref)
			next = source.SourceID
		}
		return nil
	})
	if err != nil {
		return nil, "", canonicalStoreError("list canonical source refs", err)
	}
	if len(refs) < int(limit) {
		next = ""
	}
	return refs, next, nil
}

func (s *CanonicalStore) ListCanonicalRevisionRefs(ctx context.Context, scope boyan.Scope, afterRevisionID string, limit uint32) ([]boyan.RevisionRef, string, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, "", err
	}
	if err := scope.Validate(); err != nil {
		return nil, "", err
	}
	if limit == 0 || limit > 512 || (afterRevisionID != "" && strings.TrimSpace(afterRevisionID) != afterRevisionID) {
		return nil, "", boyan.PermanentError(boyan.CodeLimitExceeded, "canonical revision enumeration limit is invalid", nil)
	}
	if s == nil {
		return nil, "", boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return nil, "", boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	prefix, err := canonicalPrefix(scope, badgerRecordRevision)
	if err != nil {
		return nil, "", err
	}
	refs := make([]boyan.RevisionRef, 0, limit)
	next := ""
	err = s.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix) && uint32(len(refs)) < limit; iterator.Next() {
			var revision boyan.MemoryRevision
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordRevision, &revision); err != nil {
				return err
			}
			if revision.RevisionID <= afterRevisionID {
				continue
			}
			deniedKey, keyErr := canonicalKey(scope, badgerRecordDeniedRevision, revision.RevisionID)
			if keyErr != nil {
				return keyErr
			}
			var denied badgerDeniedRevision
			if deniedErr := getCanonicalRecord(txn, deniedKey, badgerRecordDeniedRevision, &denied); deniedErr == nil {
				continue
			} else if !errors.Is(deniedErr, badger.ErrKeyNotFound) {
				return deniedErr
			}
			refs = append(refs, boyan.RevisionRef{ItemID: revision.ItemID, RevisionID: revision.RevisionID})
			next = revision.RevisionID
		}
		return nil
	})
	if err != nil {
		return nil, "", canonicalStoreError("list canonical revision refs", err)
	}
	if len(refs) < int(limit) {
		next = ""
	}
	return refs, next, nil
}

func (s *CanonicalStore) CanonicalSourceID(ctx context.Context, scope boyan.Scope, source boyan.SourceRef) (string, error) {
	if err := source.Validate(); err != nil {
		return "", err
	}
	if source.Scope != scope {
		return "", boyan.PermanentError(boyan.CodeScopeViolation, "canonical source scope does not match", nil)
	}
	if s == nil {
		return "", boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return "", boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	// Native turn sources have a deterministic identity. Check it first to
	// avoid scanning payload records in the common path.
	nativeID := boyan.TurnSourceID(source.ExportID)
	key, err := canonicalKey(scope, badgerRecordSource, nativeID)
	if err != nil {
		return "", err
	}
	var native boyan.SourceRecordV2
	if readErr := s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordSource, &native)
	}); readErr == nil {
		return nativeID, nil
	} else if !errors.Is(readErr, badger.ErrKeyNotFound) {
		return "", canonicalStoreError("resolve canonical source identity", readErr)
	}
	prefix, err := canonicalPrefix(scope, badgerRecordSource)
	if err != nil {
		return "", err
	}
	var resolved string
	if scanErr := s.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
			var record boyan.SourceRecordV2
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordSource, &record); err != nil {
				return err
			}
			payloadKey, keyErr := canonicalKey(payloadScope(), badgerRecordPayload, record.Payload.ID)
			if keyErr != nil {
				return keyErr
			}
			var payload []byte
			if err := getCanonicalRecord(txn, payloadKey, badgerRecordPayload, &payload); err != nil {
				return err
			}
			candidate, _, ok := canonicalSourceRef(payload, scope, record.SourceID)
			if ok && candidate == source {
				resolved = record.SourceID
				return nil
			}
		}
		return nil
	}); scanErr != nil {
		return "", canonicalStoreError("resolve canonical source identity", scanErr)
	}
	if resolved != "" {
		return resolved, nil
	}
	return "", boyan.PermanentError(boyan.CodeNotFound, "canonical source does not exist", nil)
}

func (s *CanonicalStore) LoadCanonicalForgetOutcome(ctx context.Context, scope boyan.Scope, operationID string, kind boyan.ForgetKind) (boyan.ForgetOutcome, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return boyan.ForgetOutcome{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return boyan.ForgetOutcome{}, false, err
	}
	if err := kind.Validate(); err != nil || strings.TrimSpace(operationID) == "" || strings.TrimSpace(operationID) != operationID {
		return boyan.ForgetOutcome{}, false, boyan.PermanentError(boyan.CodeInvalidDerived, "canonical forget outcome lookup is invalid", err)
	}
	if s == nil {
		return boyan.ForgetOutcome{}, false, boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return boyan.ForgetOutcome{}, false, boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := badgerForgetOutcomeKey(scope, kind, operationID)
	if err != nil {
		return boyan.ForgetOutcome{}, false, err
	}
	var stored badgerCanonicalForgetOutcome
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordForgetOutcome, &stored)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return boyan.ForgetOutcome{}, false, nil
	}
	if err != nil {
		return boyan.ForgetOutcome{}, false, canonicalStoreError("load canonical forget outcome", err)
	}
	if err := stored.Outcome.Validate(); err != nil || stored.Outcome.Scope != scope || stored.Outcome.OperationID != operationID || stored.Outcome.Kind != kind {
		return boyan.ForgetOutcome{}, false, boyan.PermanentError(boyan.CodeStoreFailure, "stored canonical forget outcome is invalid", err)
	}
	return stored.Outcome, true, nil
}

func (s *CanonicalStore) CommitCanonicalForget(ctx context.Context, request boyan.CanonicalForgetCommitRequest) (boyan.ForgetOutcome, error) {
	if err := canonicalContextError(ctx); err != nil {
		return boyan.ForgetOutcome{}, err
	}
	if err := request.Validate(); err != nil {
		return boyan.ForgetOutcome{}, err
	}
	if s == nil {
		return boyan.ForgetOutcome{}, boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return boyan.ForgetOutcome{}, boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := badgerForgetOutcomeKey(request.Scope, request.Kind, request.OperationID)
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	scopeKey, err := badgerScopeKey(request.Scope)
	if err != nil {
		return boyan.ForgetOutcome{}, err
	}
	var outcome boyan.ForgetOutcome
	err = s.db.Update(func(txn *badger.Txn) error {
		var existing badgerCanonicalForgetOutcome
		readErr := getCanonicalRecord(txn, key, badgerRecordForgetOutcome, &existing)
		if readErr == nil {
			if existing.Fingerprint != request.Fingerprint {
				return boyan.PermanentError(boyan.CodeConflict, "canonical forget operation identity was reused", nil)
			}
			if err := existing.Outcome.Validate(); err != nil || existing.Outcome.Scope != request.Scope || existing.Outcome.OperationID != request.OperationID || existing.Outcome.Kind != request.Kind {
				return boyan.PermanentError(boyan.CodeStoreFailure, "stored canonical forget outcome is invalid", err)
			}
			outcome = existing.Outcome
			return nil
		}
		if !errors.Is(readErr, badger.ErrKeyNotFound) {
			return readErr
		}
		state := boyan.ScopeState{SchemaVersion: boyan.CanonicalSchemaVersionV1, Scope: request.Scope}
		stateErr := getCanonicalRecord(txn, scopeKey, badgerRecordScope, &state)
		if stateErr != nil && !errors.Is(stateErr, badger.ErrKeyNotFound) {
			return stateErr
		}
		if err := state.Validate(); err != nil {
			return err
		}
		if state.Version != request.ExpectedScopeVersion {
			return boyan.RetryableError(boyan.CodeConflict, "canonical forget scope version changed", nil)
		}
		state.Version++
		state.ChangeSeq++
		outcome = boyan.ForgetOutcome{SchemaVersion: boyan.DerivedSchemaVersionV1, OperationID: request.OperationID, Kind: request.Kind, Scope: request.Scope, ScopeVersion: state.Version, Sources: append([]boyan.SourceRef(nil), request.Sources...), Revisions: append([]boyan.RevisionRef(nil), request.Revisions...)}
		if err := outcome.Validate(); err != nil {
			return err
		}
		changeKey, keyErr := badgerScopeChangeKey(request.Scope, state.ChangeSeq)
		if keyErr != nil {
			return keyErr
		}
		change := boyan.ScopeChange{Sequence: state.ChangeSeq, OperationID: request.OperationID, OccurredAt: request.ForgottenAt.UTC()}
		for _, revision := range request.Revisions {
			change.RevisionIDs = append(change.RevisionIDs, revision.RevisionID)
		}
		if err := putCanonicalRecord(txn, changeKey, badgerRecordChange, change); err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, scopeKey, badgerRecordScope, state); err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, key, badgerRecordForgetOutcome, badgerCanonicalForgetOutcome{Fingerprint: request.Fingerprint, Outcome: outcome}); err != nil {
			return err
		}
		if s.beforeCanonicalMutationCommit != nil {
			return s.beforeCanonicalMutationCommit()
		}
		return nil
	})
	if err != nil {
		return boyan.ForgetOutcome{}, canonicalStoreError("commit canonical forget outcome", err)
	}
	return outcome, nil
}

// ScrubCanonicalForget removes only payload blobs already protected by the
// logical deny markers. Structural records, identities, and tombstone markers
// remain available for audit and replay-safe rejection.
func (s *CanonicalStore) ScrubCanonicalForget(ctx context.Context, scope boyan.Scope, sourceIDs []string, revisionIDs []string) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if len(sourceIDs) > 4096 || len(revisionIDs) > 4096 {
		return boyan.PermanentError(boyan.CodeLimitExceeded, "canonical forget scrub bound exceeded", nil)
	}
	if s == nil {
		return boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	deniedSources := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		if strings.TrimSpace(id) != id || id == "" {
			return boyan.PermanentError(boyan.CodeInvalidDerived, "canonical forget source identity is invalid", nil)
		}
		deniedSources[id] = struct{}{}
	}
	deniedRevisions := make(map[string]struct{}, len(revisionIDs))
	for _, id := range revisionIDs {
		if strings.TrimSpace(id) != id || id == "" {
			return boyan.PermanentError(boyan.CodeInvalidDerived, "canonical forget revision identity is invalid", nil)
		}
		deniedRevisions[id] = struct{}{}
	}
	s.maintenanceMu.RLock()
	if s.db == nil {
		s.maintenanceMu.RUnlock()
		return boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	refs := make([]boyan.PayloadRef, 0, len(sourceIDs)+len(revisionIDs))
	seenPayloads := make(map[string]struct{}, len(sourceIDs)+len(revisionIDs))
	scrubOverflow := false
	appendPayload := func(ref boyan.PayloadRef) {
		if ref.ID == "" {
			return
		}
		if _, exists := seenPayloads[ref.ID]; exists {
			return
		}
		if len(refs) >= maxCanonicalScrubPayloads {
			scrubOverflow = true
			return
		}
		seenPayloads[ref.ID] = struct{}{}
		refs = append(refs, ref)
	}
	if err := s.db.View(func(txn *badger.Txn) error {
		for recordType, wanted := range map[string]map[string]struct{}{badgerRecordSource: deniedSources, badgerRecordRevision: deniedRevisions} {
			prefix, err := canonicalPrefix(scope, recordType)
			if err != nil {
				return err
			}
			iterator := txn.NewIterator(badger.DefaultIteratorOptions)
			for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
				if recordType == badgerRecordSource {
					var source boyan.SourceRecordV2
					if err := getCanonicalRecord(txn, iterator.Item().Key(), recordType, &source); err != nil {
						iterator.Close()
						return err
					}
					if _, ok := wanted[source.SourceID]; ok {
						appendPayload(source.Payload)
					}
				} else {
					var revision boyan.MemoryRevision
					if err := getCanonicalRecord(txn, iterator.Item().Key(), recordType, &revision); err != nil {
						iterator.Close()
						return err
					}
					if _, ok := wanted[revision.RevisionID]; ok {
						appendPayload(revision.Payload)
					}
				}
			}
			iterator.Close()
		}
		// Message payloads are independent immutable blobs.  Removing only
		// source/revision payloads would leave forgotten turn text reachable by
		// a trace hydrator, so scan the bounded exact scope for denied sources.
		if len(deniedSources) > 0 {
			prefix, err := canonicalPrefix(scope, badgerRecordMessage)
			if err != nil {
				return err
			}
			iterator := txn.NewIterator(badger.DefaultIteratorOptions)
			for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
				var message boyan.MessageRecord
				if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordMessage, &message); err != nil {
					iterator.Close()
					return err
				}
				if _, ok := deniedSources[message.SourceID]; ok {
					appendPayload(message.Payload)
				}
			}
			iterator.Close()
		}
		return nil
	}); err != nil {
		s.maintenanceMu.RUnlock()
		return canonicalStoreError("scan canonical forget payloads", err)
	}
	s.maintenanceMu.RUnlock()
	if scrubOverflow {
		return boyan.PermanentError(boyan.CodeLimitExceeded, "canonical forget scrub payload bound exceeded", nil)
	}
	for _, ref := range refs {
		if err := s.DeletePayload(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}
