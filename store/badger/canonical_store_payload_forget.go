package badger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type badgerDeniedSource struct {
	SourceID string    `json:"source_id"`
	DeniedAt time.Time `json:"denied_at"`
}

type badgerDeniedRevision struct {
	RevisionID string    `json:"revision_id"`
	DeniedAt   time.Time `json:"denied_at"`
}

// PutPayload persists payload bytes outside structural canonical records.
func (s *CanonicalStore) PutPayload(ctx context.Context, data []byte, ref memory.PayloadRef) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if !isBadgerPayloadValid(data, ref) {
		return memory.PermanentError(memory.CodeInvalidDerived, "payload is invalid", nil)
	}
	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := canonicalKey(payloadScope(), badgerRecordPayload, ref.ID)
	if err != nil {
		return err
	}
	if err := s.db.Update(func(txn *badger.Txn) error {
		return putCanonicalImmutableRecord(txn, key, badgerRecordPayload, data)
	}); err != nil {
		return canonicalStoreError("store payload", err)
	}
	return nil
}

// LoadPayload reads a content-free reference's payload bytes.
func (s *CanonicalStore) LoadPayload(ctx context.Context, ref memory.PayloadRef) ([]byte, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := canonicalKey(payloadScope(), badgerRecordPayload, ref.ID)
	if err != nil {
		return nil, err
	}
	var data []byte
	if err := s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordPayload, &data)
	}); err != nil {
		return nil, canonicalStoreError("load payload", err)
	}
	if !isBadgerPayloadValid(data, ref) {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "stored payload is invalid", nil)
	}
	return data, nil
}

// DeletePayload removes a payload blob after its logical forget state has
// committed. Structural tombstones remain intact.
func (s *CanonicalStore) DeletePayload(ctx context.Context, ref memory.PayloadRef) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := canonicalKey(payloadScope(), badgerRecordPayload, ref.ID)
	if err != nil {
		return err
	}
	if err := s.db.Update(func(txn *badger.Txn) error { return txn.Delete(key) }); err != nil {
		return canonicalStoreError("delete payload", err)
	}
	return nil
}

// DenySource commits the fail-closed logical forget marker before any
// asynchronous provenance traversal or payload scrubbing begins.
func (s *CanonicalStore) DenySource(ctx context.Context, scope memory.Scope, sourceID string, deniedAt time.Time) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(sourceID) != sourceID || sourceID == "" || deniedAt.IsZero() {
		return memory.PermanentError(memory.CodeInvalidDerived, "source deny request is invalid", nil)
	}
	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := canonicalKey(scope, badgerRecordDeniedSource, sourceID)
	if err != nil {
		return err
	}
	denied := badgerDeniedSource{SourceID: sourceID, DeniedAt: deniedAt.UTC()}
	if err := s.db.Update(func(txn *badger.Txn) error {
		return putBadgerDenyRecord(txn, key, badgerRecordDeniedSource, denied)
	}); err != nil {
		return canonicalStoreError("deny canonical source", err)
	}
	return nil
}

// IsSourceDenied lets recall and traversal fail closed before physical scrub.
func (s *CanonicalStore) IsSourceDenied(ctx context.Context, scope memory.Scope, sourceID string) (bool, error) {
	var denied badgerDeniedSource
	return s.isDenied(ctx, scope, sourceID, badgerRecordDeniedSource, "read canonical source deny", &denied)
}

// DenyRevision commits a fail-closed cascade result for one revision.
func (s *CanonicalStore) DenyRevision(ctx context.Context, scope memory.Scope, revisionID string, deniedAt time.Time) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if revisionID == "" || deniedAt.IsZero() {
		return memory.PermanentError(memory.CodeInvalidDerived, "revision deny request is invalid", nil)
	}
	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := canonicalKey(scope, badgerRecordDeniedRevision, revisionID)
	if err != nil {
		return err
	}
	denied := badgerDeniedRevision{RevisionID: revisionID, DeniedAt: deniedAt.UTC()}
	if err := s.db.Update(func(txn *badger.Txn) error {
		return putBadgerDenyRecord(txn, key, badgerRecordDeniedRevision, denied)
	}); err != nil {
		return canonicalStoreError("deny canonical revision", err)
	}
	return nil
}

func putBadgerDenyRecord(txn *badger.Txn, key []byte, recordType string, value any) error {
	if _, err := txn.Get(key); err == nil {
		return nil
	} else if !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	return putCanonicalRecord(txn, key, recordType, value)
}

// IsRevisionDenied lets recall fail closed while physical scrub is pending.
func (s *CanonicalStore) IsRevisionDenied(ctx context.Context, scope memory.Scope, revisionID string) (bool, error) {
	var denied badgerDeniedRevision
	return s.isDenied(ctx, scope, revisionID, badgerRecordDeniedRevision, "read canonical revision deny", &denied)
}

func (s *CanonicalStore) isDenied(ctx context.Context, scope memory.Scope, id, recordType, operation string, denied any) (bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return false, err
	}
	if s == nil {
		return false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := canonicalKey(scope, recordType, id)
	if err != nil {
		return false, err
	}
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, recordType, denied)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, canonicalStoreError(operation, err)
	}
	return true, nil
}

// SourceRevisionBatch returns a bounded, resumable source-provenance batch.
// The next cursor is the final revision ID returned; an empty cursor means
// the source has no further indexed descendants in this batch direction.
func (s *CanonicalStore) SourceRevisionBatch(ctx context.Context, scope memory.Scope, sourceID, afterRevisionID string, limit uint32) ([]string, string, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, "", err
	}
	if err := scope.Validate(); err != nil {
		return nil, "", err
	}
	if sourceID == "" || limit == 0 || limit > 512 {
		return nil, "", memory.PermanentError(memory.CodeLimitExceeded, "source provenance batch limit is invalid", nil)
	}
	if s == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	prefix, err := badgerProvenancePrefix(scope, badgerRecordSourceRevision, sourceID)
	if err != nil {
		return nil, "", err
	}
	results := make([]string, 0, limit)
	err = s.db.View(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix
		iterator := txn.NewIterator(options)
		defer iterator.Close()
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix) && uint32(len(results)) < limit; iterator.Next() {
			var edge badgerProvenanceEdge
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordSourceRevision, &edge); err != nil {
				return err
			}
			if edge.ChildRevisionID > afterRevisionID {
				results = append(results, edge.ChildRevisionID)
			}
		}
		return nil
	})
	if err != nil {
		return nil, "", canonicalStoreError("scan source provenance", err)
	}
	if len(results) == 0 {
		return results, "", nil
	}
	return results, results[len(results)-1], nil
}

func isBadgerPayloadValid(data []byte, ref memory.PayloadRef) bool {
	if ref.Validate() != nil || len(data) == 0 || len(data) != int(ref.ByteSize) {
		return false
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]) == ref.Digest
}
