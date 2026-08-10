package badger

import (
	"context"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ScrubCanonicalForget removes only payload blobs already protected by the
// logical deny markers. Structural records, identities, and tombstone markers
// remain available for audit and replay-safe rejection.
func (s *CanonicalStore) ScrubCanonicalForget(ctx context.Context, scope memory.Scope, sourceIDs []string, revisionIDs []string) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if len(sourceIDs) > 4096 || len(revisionIDs) > 4096 {
		return memory.PermanentError(memory.CodeLimitExceeded, "canonical forget scrub bound exceeded", nil)
	}
	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	deniedSources := make(map[string]struct{}, len(sourceIDs))
	for _, id := range sourceIDs {
		if strings.TrimSpace(id) != id || id == "" {
			return memory.PermanentError(memory.CodeInvalidDerived, "canonical forget source identity is invalid", nil)
		}
		deniedSources[id] = struct{}{}
	}
	deniedRevisions := make(map[string]struct{}, len(revisionIDs))
	for _, id := range revisionIDs {
		if strings.TrimSpace(id) != id || id == "" {
			return memory.PermanentError(memory.CodeInvalidDerived, "canonical forget revision identity is invalid", nil)
		}
		deniedRevisions[id] = struct{}{}
	}
	s.maintenanceMu.RLock()
	if s.db == nil {
		s.maintenanceMu.RUnlock()
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	refs := make([]memory.PayloadRef, 0, len(sourceIDs)+len(revisionIDs))
	seenPayloads := make(map[string]struct{}, len(sourceIDs)+len(revisionIDs))
	scrubOverflow := false
	appendPayload := func(ref memory.PayloadRef) {
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
		for recordType, wanted := range map[string]map[string]struct{}{
			badgerRecordSource:   deniedSources,
			badgerRecordRevision: deniedRevisions,
		} {
			prefix, err := canonicalPrefix(scope, recordType)
			if err != nil {
				return err
			}
			iterator := txn.NewIterator(badger.DefaultIteratorOptions)
			for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
				if recordType == badgerRecordSource {
					var source memory.CanonicalSourceRecord
					if err := getCanonicalRecord(txn, iterator.Item().Key(), recordType, &source); err != nil {
						iterator.Close()
						return err
					}
					if _, ok := wanted[source.SourceID]; ok {
						appendPayload(source.Payload)
					}
				} else {
					var revision memory.MemoryRevision
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
				var message memory.MessageRecord
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
		return memory.PermanentError(memory.CodeLimitExceeded, "canonical forget scrub payload bound exceeded", nil)
	}
	for _, ref := range refs {
		if err := s.DeletePayload(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}
