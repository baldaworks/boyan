package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Fixed dispatch inventory, not an extensible persistence registry.
var durableFamilies = [...]Family{
	ScopeState, Operation, Source, Message, Item, Revision, Lifecycle, Head, ScopeChange,
	Delivery, DeliveryClaim, Payload, ProvenanceForward, ProvenanceReverse, SourceRevision,
	DeniedSource, DeniedRevision, ProjectionManifest, ProjectionActive, ProjectionRetention,
	ProjectionCheckpoint, ForgetOutcome, IngestSequence, IngestCursor, TurnCertificate,
	BoundaryCertificate, BoundaryCapability, SourcePrivacy, SourcePrivacyScope, EraseJournal,
	EraseFence, EraseSourceIdentity, SourcePrivacyReady, EraseGuard, EraseSourceRef, EraseLockRef,
	EraseBlobLock, ErasedBlob,
}

// CheckIntegrity validates structural persistence bindings and row/stamp coverage
// in bounded pages. It does not replace owner/domain/archive validation. Call it
// in the same transaction before readiness/import completion; Migrate also calls it.
func (tx *Tx) CheckIntegrity(ctx context.Context) error {
	return tx.checkIntegrity(ctx, memory.MaxCanonicalReadRecords)
}

func (tx *Tx) checkIntegrity(ctx context.Context, pageSize int) error {
	if err := tx.check(ctx, false); err != nil {
		return err
	}

	for _, family := range durableFamilies {
		if err := tx.checkFamilyIntegrity(ctx, family, pageSize); err != nil {
			return err
		}
	}

	return tx.checkStampIntegrity(ctx, pageSize)
}

func (tx *Tx) checkStampIntegrity(ctx context.Context, pageSize int) error {
	var encoded []byte
	if err := scanOne(ctx, tx.q, "SELECT mutation_clock FROM "+tx.connection.table("store_meta")+" WHERE singleton=1",
		nil, &encoded); err != nil {
		return tx.fail(databaseError(ctx, "integrity clock", err))
	}

	clock, err := DecodeUint64(encoded)
	if err != nil {
		return tx.fail(corruptRow())
	}

	var after *StampCursor
	for {
		page, err := tx.ScanStamps(ctx, after, pageSize)
		if err != nil {
			return err
		}

		if len(page) == 0 {
			return nil
		}

		for _, stamp := range page {
			if stamp.Cursor.Sequence > clock {
				return tx.fail(corruptRow())
			}
		}

		cursor := page[len(page)-1].Cursor
		after = &cursor
	}
}

func (tx *Tx) checkFamilyIntegrity(ctx context.Context, family Family, pageSize int) error {
	var after *Key
	for {
		page, err := tx.Scan(ctx, Range{Family: family, After: after, Limit: pageSize})
		if err != nil {
			return err
		}

		if len(page) == 0 {
			return nil
		}

		for _, row := range page {
			_, values, _ := row.Key.values()

			var count int

			err := scanOne(ctx, tx.q, "SELECT count(*) FROM (SELECT 1 FROM "+tx.connection.table("row_stamp")+
				" WHERE family=$1 AND binary_key=$2 AND deleted=$3 LIMIT 2) live",
				[]any{[]byte(family), stampKey(values), false}, &count)
			if err != nil {
				return tx.fail(databaseError(ctx, "integrity stamp", err))
			}

			if count != 1 {
				return tx.fail(corruptRow())
			}
		}

		key := page[len(page)-1].Key
		after = &key
	}
}
