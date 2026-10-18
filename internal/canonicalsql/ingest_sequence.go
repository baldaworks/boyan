package canonicalsql

import (
	"context"
	"encoding/binary"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.IngestSequenceStore = (*Owner)(nil)

// LoadLatestIngestSequence reads the exact producer/scope cursor in one snapshot.
func (o *Owner) LoadLatestIngestSequence(ctx context.Context, producerID string,
	scope memory.Scope) (memory.IngestSequenceCursor, bool, error) {
	if err := validateIngestLookup(ctx, producerID, scope); err != nil {
		return memory.IngestSequenceCursor{}, false, err
	}

	var (
		cursor memory.IngestSequenceCursor
		found  bool
	)

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		cursor, found, err = loadIngestCursor(ctx, tx, producerID, scope)

		return err
	})
	if err != nil {
		return memory.IngestSequenceCursor{}, false, err
	}

	return cursor, found, nil
}

// LoadIngestSequence returns the original accepted receipt, without rewriting it.
func (o *Owner) LoadIngestSequence(ctx context.Context, producerID string, scope memory.Scope,
	sequence uint64) (memory.IngestSequenceRecord, bool, error) {
	if sequence == 0 {
		return memory.IngestSequenceRecord{}, false, invalid("ingest sequence lookup is invalid")
	}

	if err := validateIngestLookup(ctx, producerID, scope); err != nil {
		return memory.IngestSequenceRecord{}, false, err
	}

	var (
		record memory.IngestSequenceRecord
		found  bool
	)

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		record, found, err = loadIngestRecord(ctx, tx, producerID, scope, sequence)

		return err
	})
	if err != nil {
		return memory.IngestSequenceRecord{}, false, err
	}

	return record, found, nil
}

func validateIngestLookup(ctx context.Context, producerID string, scope memory.Scope) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if err := scope.Validate(); err != nil {
		return err
	}

	if producerID == "" || producerID != strings.TrimSpace(producerID) || strings.ContainsAny(producerID, "\r\n\t") {
		return invalid("ingest producer id is invalid")
	}

	return nil
}

func ingestSequenceKey(scope memory.Scope, producerID string, sequence uint64) Key {
	return Key{Family: IngestSequence, Scope: scope,
		Parts: [][]byte{[]byte(producerID), binary.BigEndian.AppendUint64(nil, sequence)}}
}

func loadIngestCursor(ctx context.Context, tx *Tx, producerID string,
	scope memory.Scope) (memory.IngestSequenceCursor, bool, error) {
	key, err := privacyKey(scope, IngestCursor, producerID)
	if err != nil {
		return memory.IngestSequenceCursor{}, false, err
	}

	var cursor memory.IngestSequenceCursor

	err = getPrivacyRecord(ctx, tx, key, IngestCursor, &cursor)
	if privacyMissing(err) {
		return memory.IngestSequenceCursor{}, false, nil
	}

	if err != nil {
		return memory.IngestSequenceCursor{}, false, err
	}

	if err := cursor.Validate(); err != nil || cursor.ProducerID != producerID || cursor.Scope != scope {
		return memory.IngestSequenceCursor{}, false, tx.fail(proofStoreFailure("stored ingest cursor is invalid", err))
	}

	return cursor, true, nil
}

func loadIngestRecord(ctx context.Context, tx *Tx, producerID string, scope memory.Scope,
	sequence uint64) (memory.IngestSequenceRecord, bool, error) {
	var record memory.IngestSequenceRecord

	err := getPrivacyRecord(ctx, tx, ingestSequenceKey(scope, producerID, sequence), IngestSequence, &record)
	if privacyMissing(err) {
		return memory.IngestSequenceRecord{}, false, nil
	}

	if err != nil {
		return memory.IngestSequenceRecord{}, false, err
	}

	if err := record.Validate(); err != nil || record.ProducerID != producerID || record.Scope != scope ||
		record.Sequence != sequence {
		return memory.IngestSequenceRecord{}, false, tx.fail(proofStoreFailure("stored ingest receipt is invalid", err))
	}

	return record, true, nil
}
