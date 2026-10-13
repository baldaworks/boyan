package badger

import (
	"bytes"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// Scan one bounded slice of a prior outcome's already bounded references.
func eraseReadOutcomePage(txn *badger.Txn, journal *memory.ScopeEraseJournal) error {
	prefix, err := canonicalPrefix(journal.Scope, badgerRecordForgetOutcome)
	if err != nil {
		return err
	}

	if len(journal.Cursor) > 0 && !bytes.HasPrefix(journal.Cursor, prefix) {
		return eraseStoreFailure("scope erase outcome cursor differs", nil)
	}

	options := badger.DefaultIteratorOptions
	options.PrefetchValues = false

	iterator := txn.NewIterator(options)
	defer iterator.Close()

	seek := prefix
	if len(journal.Cursor) > 0 {
		seek = journal.Cursor
	}

	iterator.Seek(seek)

	if journal.ReferenceOffset == 0 && iterator.ValidForPrefix(
		prefix) && bytes.Equal(iterator.Item().Key(),
		journal.Cursor,
	) {
		iterator.Next()
	}

	if !iterator.ValidForPrefix(prefix) {
		eraseTransition(journal, memory.ScopeEraseLockSources)

		return nil
	}

	key := iterator.Item().KeyCopy(nil)

	return eraseCopyOutcomeRefs(txn, journal, key)
}

func eraseCopyOutcomeRefs(txn *badger.Txn, journal *memory.ScopeEraseJournal, key []byte) error {
	var stored badgerCanonicalForgetOutcome
	if err := getCanonicalRecord(txn, key, badgerRecordForgetOutcome, &stored); err != nil {
		return err
	}

	if err := stored.Outcome.Validate(); err != nil {
		return eraseStoreFailure("invalid prior forget outcome", err)
	}

	if stored.Outcome.Scope != journal.Scope {
		return eraseStoreFailure("prior forget outcome scope differs", nil)
	}

	start := int(journal.ReferenceOffset)
	if start > len(stored.Outcome.Sources) {
		return eraseStoreFailure("scope erase reference offset differs", nil)
	}

	end := min(start+scopeEraseBatchSize, len(stored.Outcome.Sources))
	for _, ref := range stored.Outcome.Sources[start:end] {
		identityKey, err := eraseOwnedKey(*journal, badgerRecordEraseSourceRef, memory.TurnSourceID(ref.ExportID))
		if err != nil {
			return err
		}

		if err := putCanonicalImmutableRecord(txn, identityKey, badgerRecordEraseSourceRef, ref); err != nil {
			return err
		}
	}

	journal.Cursor = key

	journal.ReferenceOffset = uint32(end)
	if end == len(stored.Outcome.Sources) {
		journal.ReferenceOffset = 0
	}

	return nil
}

func eraseSourceRef(
	txn *badger.Txn,
	journal memory.ScopeEraseJournal,
	source memory.CanonicalSourceRecord,
) (memory.SourceRef, error) {
	data, err := listedCanonicalSourcePayload(txn, source.Payload.ID)
	if err == nil {
		if !isBadgerPayloadValid(data, source.Payload) {
			return memory.SourceRef{}, eraseStoreFailure("scope erase source payload is invalid", nil)
		}

		ref, _, ok := canonicalSourceRef(data, journal.Scope, source.SourceID)
		if !ok {
			return memory.SourceRef{}, eraseStoreFailure("scope erase source identity is unavailable", nil)
		}

		return ref, nil
	}

	if !errors.Is(err, badger.ErrKeyNotFound) {
		return memory.SourceRef{}, err
	}

	return eraseMissingSourceRef(txn, journal, source.SourceID)
}

func eraseMissingSourceRef(
	txn *badger.Txn,
	journal memory.ScopeEraseJournal,
	sourceID string,
) (memory.SourceRef, error) {
	ref, found, err := loadErasedSourceIdentity(txn, journal.Scope, sourceID)
	if err != nil || found {
		return ref, err
	}

	key, err := eraseOwnedKey(journal, badgerRecordEraseSourceRef, sourceID)
	if err != nil {
		return memory.SourceRef{}, err
	}

	ref = memory.SourceRef{}
	if err := getCanonicalRecord(txn, key, badgerRecordEraseSourceRef, &ref); err != nil {
		return ref, eraseStoreFailure("scope erase source identity evidence is absent", err)
	}

	if err := ref.Validate(); err != nil {
		return ref, eraseStoreFailure("scope erase source identity evidence is invalid", err)
	}

	if ref.Scope != journal.Scope {
		return ref, eraseStoreFailure("scope erase source identity evidence scope differs", nil)
	}

	return ref, nil
}

func loadErasedSourceIdentity(txn *badger.Txn, scope memory.Scope, sourceID string) (memory.SourceRef, bool, error) {
	var ref memory.SourceRef

	key, err := canonicalKey(scope, badgerRecordEraseSourceIdentity, sourceID)
	if err != nil {
		return ref, false, err
	}

	err = getCanonicalRecord(txn, key, badgerRecordEraseSourceIdentity, &ref)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return ref, false, nil
	}

	if err != nil {
		return ref, false, eraseStoreFailure("decode erased source identity", err)
	}

	if ref.Scope != scope || ref.Validate() != nil {
		return ref, false, eraseStoreFailure("invalid erased source identity", nil)
	}

	return ref, true, nil
}
