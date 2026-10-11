package memory

import "context"

// IngestSequenceStore persists exact-scope producer ordering and replay.
type IngestSequenceStore interface {
	LoadLatestIngestSequence(ctx context.Context, producerID string, scope Scope) (IngestSequenceCursor, bool, error)
	LoadIngestSequence(
		ctx context.Context,
		producerID string,
		scope Scope,
		sequence uint64,
	) (
		IngestSequenceRecord,
		bool,
		error,
	)
	SaveIngestSequence(ctx context.Context, record IngestSequenceRecord) error
}
