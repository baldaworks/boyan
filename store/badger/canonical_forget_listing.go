package badger

import (
	"context"
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) ListCanonicalSourceRefs(ctx context.Context, scope memory.Scope, afterSourceID string, limit uint32) ([]memory.SourceRef, string, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, "", err
	}
	if err := scope.Validate(); err != nil {
		return nil, "", err
	}
	if limit == 0 || limit > 512 || (afterSourceID != "" && strings.TrimSpace(afterSourceID) != afterSourceID) {
		return nil, "", memory.PermanentError(memory.CodeLimitExceeded, "canonical source enumeration limit is invalid", nil)
	}
	if s == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	prefix, err := canonicalPrefix(scope, badgerRecordSource)
	if err != nil {
		return nil, "", err
	}
	refs := make([]memory.SourceRef, 0, limit)
	next := ""
	err = s.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix) && uint32(len(refs)) < limit; iterator.Next() {
			var source memory.CanonicalSourceRecord
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
				return memory.PermanentError(memory.CodeStoreFailure, "canonical source payload is invalid", nil)
			}
			ref, _, ok := canonicalSourceRef(payload, scope, source.SourceID)
			if !ok {
				return memory.PermanentError(memory.CodeInvalidDerived, "canonical source identity is not representable", nil)
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

func (s *CanonicalStore) ListCanonicalRevisionRefs(ctx context.Context, scope memory.Scope, afterRevisionID string, limit uint32) ([]memory.RevisionRef, string, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, "", err
	}
	if err := scope.Validate(); err != nil {
		return nil, "", err
	}
	if limit == 0 || limit > 512 || (afterRevisionID != "" && strings.TrimSpace(afterRevisionID) != afterRevisionID) {
		return nil, "", memory.PermanentError(memory.CodeLimitExceeded, "canonical revision enumeration limit is invalid", nil)
	}
	if s == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	prefix, err := canonicalPrefix(scope, badgerRecordRevision)
	if err != nil {
		return nil, "", err
	}
	refs := make([]memory.RevisionRef, 0, limit)
	next := ""
	err = s.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix) && uint32(len(refs)) < limit; iterator.Next() {
			var revision memory.MemoryRevision
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
			refs = append(refs, memory.RevisionRef{ItemID: revision.ItemID, RevisionID: revision.RevisionID})
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

func (s *CanonicalStore) CanonicalSourceID(ctx context.Context, scope memory.Scope, source memory.SourceRef) (string, error) {
	if err := source.Validate(); err != nil {
		return "", err
	}
	if source.Scope != scope {
		return "", memory.PermanentError(memory.CodeScopeViolation, "canonical source scope does not match", nil)
	}
	if s == nil {
		return "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	// Native turn sources have a deterministic identity. Check it first to
	// avoid scanning payload records in the common path.
	nativeID := memory.TurnSourceID(source.ExportID)
	key, err := canonicalKey(scope, badgerRecordSource, nativeID)
	if err != nil {
		return "", err
	}
	var native memory.CanonicalSourceRecord
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
			var record memory.CanonicalSourceRecord
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
	return "", memory.PermanentError(memory.CodeNotFound, "canonical source does not exist", nil)
}
