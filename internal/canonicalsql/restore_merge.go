package canonicalsql

import (
	"bytes"
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

//nolint:cyclop // Fixed-family authority gates precede ordinary replay writes.
func (tx *Tx) mergeRestoreRecord(ctx context.Context, key Key, record archive.BackupRecord, generation string) error {
	// Copied readiness is never authority; validated reconstruction publishes it.
	if key.Family == SourcePrivacyReady || key.Family == BoundaryCapability || key.Family == EraseGuard {
		return nil
	}

	current, err := tx.Get(ctx, key)
	if err != nil && !privacyMissing(err) {
		return err
	}

	found := err == nil
	if record.Deleted {
		if found && retainedRestoreFamily(key.Family) {
			return nil
		}

		return tx.Delete(ctx, key)
	}

	if key.Family == Payload {
		_, err := tx.Get(ctx, Key{Family: ErasedBlob, Parts: key.Parts})
		if err == nil {
			return tx.Delete(ctx, key)
		}

		if !privacyMissing(err) {
			return err
		}
	}

	if found {
		return tx.mergeExistingRestoreRecord(ctx, key, current, record.Envelope)
	}

	if err := tx.checkRestoreOperationSource(ctx, key, generation); err != nil {
		return err
	}

	if key.Family == ErasedBlob {
		if err := tx.Delete(ctx, Key{Family: Payload, Parts: key.Parts}); err != nil {
			return err
		}
	}

	return tx.Put(ctx, key, record.Envelope)
}

func retainedRestoreFamily(f Family) bool {
	switch f {
	case ScopeState, DeniedSource, DeniedRevision, ErasedBlob, SourcePrivacy, SourcePrivacyScope,
		TurnCertificate, BoundaryCertificate, BoundaryCapability, Operation, ScopeChange, IngestSequence, ForgetOutcome,
		EraseJournal, EraseFence, EraseGuard, EraseSourceRef, EraseLockRef, EraseBlobLock, EraseSourceIdentity:
		return true
	default:
		return false
	}
}

//nolint:cyclop // Preserve sensitive, unknown, incomplete and immutable membership together.
func mergeRestorePrivacy(f Family, current, incoming []byte) ([]byte, error) {
	if f == SourcePrivacyScope {
		var old, next sourcePrivacyScope
		if Decode(f, current, &old) != nil || Decode(f, incoming, &next) != nil ||
			old.Scope != next.Scope {
			return nil, invalidSourcePrivacy()
		}

		next.Incomplete = next.Incomplete || old.Incomplete

		return Encode(f, next)
	}

	var old, next sourcePrivacy
	if Decode(f, current, &old) != nil || Decode(f, incoming, &next) != nil ||
		old.Scope != next.Scope ||
		old.SourceID != next.SourceID ||
		old.Payload != next.Payload {
		return nil, invalidSourcePrivacy()
	}

	if old.State == sourcePrivacySensitive {
		next.State = sourcePrivacySensitive
	}

	if old.StickyUnknown || (old.State == sourcePrivacyUnknown && !old.Preparing) {
		next.StickyUnknown = true
		if next.State != sourcePrivacySensitive {
			next.State = sourcePrivacyUnknown
		}
	}

	next.Incomplete = next.Incomplete || old.Incomplete
	// Complete destination captures are immutable membership evidence. A retry
	// may replace an incomplete capture, but must not enlarge a certified subset.
	if old.MessageMembershipComplete && !old.Preparing &&
		!samePrivacyMemberIDs(old.MessageIDs, next.MessageIDs) {
		return nil, invalidSourcePrivacy()
	}

	return Encode(f, next)
}

//nolint:cyclop // Exhaustive policy for an existing immutable or restrictive identity.
func (tx *Tx) mergeExistingRestoreRecord(
	ctx context.Context,
	key Key,
	current, incoming []byte,
) error {
	if bytes.Equal(current, incoming) {
		return nil
	}

	switch key.Family {
	case ScopeState:
		keep, err := keepRestoreScope(current, incoming)
		if err != nil || keep {
			return err
		}
	case SourcePrivacy, SourcePrivacyScope:
		merged, err := mergeRestorePrivacy(key.Family, current, incoming)
		if err != nil {
			return err
		}

		return tx.Put(ctx, key, merged)
	case DeniedSource, DeniedRevision, ErasedBlob:
		return nil
	case Source, Message, Item, Revision, Payload, TurnCertificate, BoundaryCertificate, Operation,
		ScopeChange, IngestSequence, ForgetOutcome, EraseSourceIdentity, EraseFence, EraseLockRef, EraseBlobLock:
		return invalid("canonical restore immutable identity conflict")
	case EraseJournal:
		var old, next memory.ScopeEraseJournal
		if Decode(EraseJournal, current, &old) != nil ||
			Decode(EraseJournal, incoming, &next) != nil {
			return corruptRow()
		}

		if !sameRestoreEraseIdentity(old, next) || eraseTerminal(old.Stage) {
			return invalid("canonical restore erase identity or terminal progress conflict")
		}
	}

	return tx.Put(ctx, key, incoming)
}

func keepRestoreScope(current, incoming []byte) (bool, error) {
	var old, next memory.ScopeState
	if Decode(ScopeState, current, &old) != nil || Decode(ScopeState, incoming, &next) != nil {
		return false, corruptRow()
	}

	if old.Version >= next.Version && old.ChangeSeq >= next.ChangeSeq {
		return true, nil
	}

	if old.Version > next.Version || old.ChangeSeq > next.ChangeSeq {
		return false, invalid("canonical restore scope clocks conflict")
	}

	return false, nil
}
