package canonicalsql

import (
	"context"
	"reflect"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// SaveIngestSequence commits the receipt and cursor together, separately from
// the canonical mutation so ordered ingest can recover an interrupted handoff.
func (o *Owner) SaveIngestSequence(ctx context.Context, record memory.IngestSequenceRecord) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if err := record.Validate(); err != nil {
		return err
	}

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		return saveIngestSequence(ctx, tx, record)
	})
}

func saveIngestSequence(ctx context.Context, tx *Tx, record memory.IngestSequenceRecord) error {
	stored, found, err := loadIngestRecord(ctx, tx, record.ProducerID, record.Scope, record.Sequence)
	if err != nil {
		return err
	}

	if found {
		if stored.Fingerprint != record.Fingerprint || !reflect.DeepEqual(stored.Outcome, record.Outcome) {
			return memory.PermanentError(memory.CodeConflict, "ingest sequence identity was reused", nil)
		}

		return nil
	}

	cursor, found, err := loadIngestCursor(ctx, tx, record.ProducerID, record.Scope)
	if err != nil {
		return err
	}

	if err := validateNextIngestRecord(cursor, found, record.Sequence); err != nil {
		return err
	}

	cursor = memory.IngestSequenceCursor{SchemaVersion: memory.IngestSequenceSchemaVersionV1,
		ProducerID: record.ProducerID, Scope: record.Scope, Latest: record.Sequence}
	if err := putPrivacyRecord(ctx, tx, ingestSequenceKey(record.Scope, record.ProducerID, record.Sequence),
		IngestSequence, record); err != nil {
		return err
	}

	key, err := privacyKey(record.Scope, IngestCursor, record.ProducerID)
	if err != nil {
		return err
	}

	return putPrivacyRecord(ctx, tx, key, IngestCursor, cursor)
}

func validateNextIngestRecord(cursor memory.IngestSequenceCursor, found bool, sequence uint64) error {
	if (!found && sequence != 1) || (found && (cursor.Latest == ^uint64(0) || sequence != cursor.Latest+1)) {
		return memory.PermanentError(memory.CodeConflict, "ingest sequence is not next", nil)
	}

	return nil
}
