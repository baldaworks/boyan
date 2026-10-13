package orderedingest

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *SequencedIngestRuntime) verifyBoundaryIdentity(
	ctx context.Context, boundary memory.Boundary, requireCertified bool,
) error {
	verifier, ok := r.canonical.(memory.CanonicalBoundaryIdentityStore)
	if !ok {
		if requireCertified {
			return memory.PermanentError(memory.CodeConflict, "boundary replay proof capability is unavailable", nil)
		}

		return nil
	}

	result, err := verifier.VerifyCanonicalBoundary(ctx, boundary)
	if err != nil {
		return err
	}

	if requireCertified && !result.Certified {
		return memory.PermanentError(memory.CodeConflict, "boundary replay lacks original input proof", nil)
	}

	return nil
}

func (r *SequencedIngestRuntime) loadBoundaryReplay(ctx context.Context, request memory.SequencedBoundary,
) (memory.IngestOutcome, bool, error) {
	if err := r.verifyBoundaryIdentity(ctx, request.Boundary, false); err != nil {
		return memory.IngestOutcome{}, false, err
	}

	replay, found, err := r.loadReplay(
		ctx, request.ProducerID, request.Boundary.Scope, request.ScopeSequence, request.Fingerprint,
	)
	if err != nil || !found {
		return replay, found, err
	}

	if replay.Kind != memory.IngestKindBoundary || replay.ExportID != request.Boundary.ExportID ||
		replay.Scope != request.Boundary.Scope {
		return memory.IngestOutcome{}, true,
			memory.PermanentError(memory.CodeConflict, "ingest receipt boundary identity differs", nil)
	}
	// A certificate may have appeared after the initial check and before the
	// receipt read. Immutable proof must also authorize the outcome we return.
	if err := r.verifyBoundaryIdentity(ctx, request.Boundary, true); err != nil {
		return memory.IngestOutcome{}, true, err
	}

	return replay, true, nil
}
