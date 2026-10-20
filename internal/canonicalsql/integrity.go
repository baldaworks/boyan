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

// CheckIntegrity validates structural persistence bindings
// in bounded pages. It does not replace owner/domain validation. Call it
// in the same transaction before readiness; Migrate also calls it.
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

	return nil
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

		key := page[len(page)-1].Key
		after = &key
	}
}
