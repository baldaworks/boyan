package semanticprojection

import (
	"context"
	"errors"

	semanticcore "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func (manager *Manager) finishBuild(ctx context.Context, target buildTarget, buildErr error, stageFailed bool) {
	if buildErr == nil || manager.lifecycle == nil {
		return
	}

	if err := manager.finishCandidate(ctx, target.generation.Scope, target.generation.BuildID); err != nil {
		manager.recordCleanupDeferred(target.generation.Scope)
	} else if stageFailed {
		manager.publishFailure(ctx, target, buildErr)
	}

	manager.recordBuildFailure(target.generation.Scope, target, buildErr)
}

// finishCandidate resolves even an ambiguous publication result using only
// this producer's generated identity. One detached budget covers both steps;
// storage still enforces its own lifetime cancellation.
func (manager *Manager) finishCandidate(ctx context.Context, scope memory.Scope, buildID string) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), manager.cleanupTimeout)
	defer cancel()

	result, err := manager.lifecycle.ReleaseUnpublished(cleanupCtx, scope, buildID)
	if err != nil {
		return err
	}

	switch result.Disposition {
	case semanticcore.CandidateMissing, semanticcore.CandidateReleased:
		return nil
	case semanticcore.CandidatePublished:
		if result.Epoch == 0 {
			return semanticcore.ErrCorrupt
		}

		err = manager.lifecycle.InvalidatePublication(cleanupCtx, scope, buildID, result.Epoch)
		// A replacement won after classification. Exact invalidation cannot
		// touch it and this owner's publication is already retired.
		if errors.Is(err, semanticcore.ErrConflict) {
			return nil
		}

		return err
	default:
		return semanticcore.ErrCorrupt
	}
}

func (manager *Manager) recordCleanupDeferred(scope memory.Scope) {
	manager.statusMu.Lock()
	status := manager.statuses[scopeKey(scope)]
	status.CleanupDeferred = true
	status.CleanupReason = semanticcore.ReasonUnavailable
	manager.statuses[scopeKey(scope)] = status
	manager.statusMu.Unlock()
}
