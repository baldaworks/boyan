package sqlite

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.IngestSequenceStore = (*Store)(nil)

// LoadLatestIngestSequence delegates to the admitted shared owner.
func (s *Store) LoadLatestIngestSequence(ctx context.Context, producerID string,
	scope memory.Scope) (memory.IngestSequenceCursor, bool, error) {
	return s.canonicalOwner().LoadLatestIngestSequence(ctx, producerID, scope)
}

// LoadIngestSequence delegates to the admitted shared owner.
func (s *Store) LoadIngestSequence(ctx context.Context, producerID string, scope memory.Scope,
	sequence uint64) (memory.IngestSequenceRecord, bool, error) {
	return s.canonicalOwner().LoadIngestSequence(ctx, producerID, scope, sequence)
}

// SaveIngestSequence delegates the atomic receipt/cursor save.
func (s *Store) SaveIngestSequence(ctx context.Context, record memory.IngestSequenceRecord) error {
	return s.canonicalOwner().SaveIngestSequence(ctx, record)
}
