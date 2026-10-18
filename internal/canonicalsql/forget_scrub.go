package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"strings"
)

const maxForgetScrubIDs = 4096

// ScrubCanonicalForget preserves the bounded legacy payload cleanup capability.
// Durable recoverable source/scope erases use RunScopeErase instead.
func (o *Owner) ScrubCanonicalForget(ctx context.Context, scope memory.Scope, sourceIDs, revisionIDs []string) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if err := scope.Validate(); err != nil {
		return err
	}

	if len(sourceIDs) > maxForgetScrubIDs || len(revisionIDs) > maxForgetScrubIDs {
		return limitError()
	}

	sources, err := normalizeScrubIDs(sourceIDs)
	if err != nil {
		return err
	}

	revisions, err := normalizeScrubIDs(revisionIDs)
	if err != nil {
		return err
	}

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		if err := checkScopeErasePayloadGuard(ctx, tx); err != nil {
			return err
		}

		refs, err := collectForgetScrubRefs(ctx, tx, scope, sources, revisions)
		if err != nil {
			return err
		}

		return deleteForgetScrubRefs(ctx, tx, refs)
	})
}
func normalizeScrubIDs(ids []string) (map[string]bool, error) {
	result := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || strings.TrimSpace(id) != id {
			return nil, memory.PermanentError(memory.CodeInvalidDerived, "canonical forget identity is invalid", nil)
		}

		result[id] = true
	}

	return result, nil
}

func collectForgetScrubRefs(ctx context.Context, tx *Tx, scope memory.Scope, sources,
	revisions map[string]bool) (map[string]memory.PayloadRef, error) {
	refs := make(map[string]memory.PayloadRef)

	for _, family := range []Family{Source, Message, Revision} {
		if family == Revision && len(revisions) == 0 || family != Revision && len(sources) == 0 {
			continue
		}

		err := walkForgetRefs(ctx, tx, forgetScan(scope, family, ""), maxForgetScrubIDs+1,
			func(r eraseRawRecord) (bool, error) {
				if !selectForgetScrub(r, sources, revisions) {
					return false, nil
				}

				refs[r.Payload.ID] = r.Payload
				if len(refs) > maxForgetScrubIDs {
					return false, limitError()
				}

				return false, nil
			})
		if err != nil {
			return nil, err
		}
	}

	return refs, nil
}
func selectForgetScrub(r eraseRawRecord, sources, revisions map[string]bool) bool {
	if r.Message != nil {
		return sources[r.Message.SourceID]
	}

	if r.Revision != nil {
		return revisions[r.ID]
	}

	return sources[r.ID]
}

func deleteForgetScrubRefs(ctx context.Context, tx *Tx, refs map[string]memory.PayloadRef) error {
	for _, ref := range refs {
		if err := checkEraseBlobLock(ctx, tx, ref.ID); err != nil {
			return err
		}

		key, err := payloadRecordKey(ref)
		if err != nil {
			return err
		}

		if err := tx.Delete(ctx, key); err != nil {
			return err
		}
	}

	return nil
}
