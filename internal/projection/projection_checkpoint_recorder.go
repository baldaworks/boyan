package projection

import (
	"context"
	"fmt"

	. "github.com/baldaworks/boyan/pkg/memory"
)

// ScopeCheckpointRecorder is the lane-owned adapter used by turn, token,
// timer, and boundary triggers. It makes replay of the same cursor idempotent
// and rejects out-of-order writes before they reach durable storage.
type ScopeCheckpointRecorder struct {
	store ScopeCheckpointStore
}

func NewScopeCheckpointRecorder(store ScopeCheckpointStore) (*ScopeCheckpointRecorder, error) {
	if store == nil {
		return nil, fmt.Errorf("scope checkpoint store is required")
	}

	return &ScopeCheckpointRecorder{store: store}, nil
}

func (r *ScopeCheckpointRecorder) Record(ctx context.Context, checkpoint ScopeCheckpoint) error {
	if r == nil || r.store == nil {
		return fmt.Errorf("scope checkpoint recorder is required")
	}

	if err := checkpoint.Validate(); err != nil {
		return err
	}

	prior, found, err := r.store.LoadScopeCheckpoint(ctx, checkpoint.Scope, checkpoint.Kind)
	if err != nil {
		return err
	}

	if found {
		replay, err := validateCheckpointProgress(prior, checkpoint)
		if err != nil || replay {
			return err
		}
	}

	return r.store.SaveScopeCheckpoint(ctx, checkpoint)
}

func validateCheckpointProgress(prior, checkpoint ScopeCheckpoint) (bool, error) {
	if err := prior.Validate(); err != nil {
		return false, err
	}

	if prior.Scope != checkpoint.Scope || prior.Kind != checkpoint.Kind {
		return false, PermanentError(CodeScopeViolation, "scope checkpoint ownership changed", nil)
	}

	if checkpoint.ScopeVersion < prior.ScopeVersion ||
		checkpoint.ChangeSeq < prior.ChangeSeq ||
		checkpoint.OccurredAt.Before(prior.OccurredAt) {
		return false, PermanentError(CodeConflict, "scope checkpoint moved backwards", nil)
	}

	if checkpoint.ScopeVersion == prior.ScopeVersion &&
		checkpoint.ChangeSeq == prior.ChangeSeq &&
		checkpoint.CheckpointID == prior.CheckpointID {
		return true, nil
	}

	return false, nil
}
