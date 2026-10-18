package canonicalsql

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.ScopeCheckpointStore = (*Owner)(nil)

// LoadScopeCheckpoint reads the latest exact-scope trigger cursor.
func (o *Owner) LoadScopeCheckpoint(
	ctx context.Context,
	scope memory.Scope,
	kind memory.ScopeCheckpointKind,
) (memory.ScopeCheckpoint, bool, error) {
	if err := mutationContext(ctx); err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}

	if err := scope.Validate(); err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}

	if err := kind.Validate(); err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}

	key, err := privacyKey(scope, ProjectionCheckpoint, string(kind))
	if err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}

	var (
		checkpoint memory.ScopeCheckpoint
		found      bool
	)

	err = o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		checkpoint, found, err = loadScopeCheckpoint(ctx, tx, key, scope, kind)

		return err
	})
	if err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}

	return checkpoint, found, nil
}

func loadScopeCheckpoint(
	ctx context.Context,
	tx *Tx,
	key Key,
	scope memory.Scope,
	kind memory.ScopeCheckpointKind,
) (memory.ScopeCheckpoint, bool, error) {
	var checkpoint memory.ScopeCheckpoint

	err := getPrivacyRecord(ctx, tx, key, ProjectionCheckpoint, &checkpoint)
	if privacyMissing(err) {
		return memory.ScopeCheckpoint{}, false, nil
	}

	if err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}

	if err := checkpoint.Validate(); err != nil || checkpoint.Scope != scope || checkpoint.Kind != kind {
		return memory.ScopeCheckpoint{}, false, proofStoreFailure("stored scope checkpoint is invalid", err)
	}

	return checkpoint, true, nil
}

// SaveScopeCheckpoint rejects backwards cursors without advancing history.
func (o *Owner) SaveScopeCheckpoint(ctx context.Context, checkpoint memory.ScopeCheckpoint) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if err := checkpoint.Validate(); err != nil {
		return err
	}

	key, err := privacyKey(checkpoint.Scope, ProjectionCheckpoint, string(checkpoint.Kind))
	if err != nil {
		return err
	}

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		return saveScopeCheckpoint(ctx, tx, key, checkpoint)
	})
}

func saveScopeCheckpoint(ctx context.Context, tx *Tx, key Key, checkpoint memory.ScopeCheckpoint) error {
	var current memory.ScopeCheckpoint

	err := getPrivacyRecord(ctx, tx, key, ProjectionCheckpoint, &current)
	if err == nil {
		if current.ScopeVersion > checkpoint.ScopeVersion || current.ChangeSeq > checkpoint.ChangeSeq ||
			current.OccurredAt.After(checkpoint.OccurredAt) {
			return memory.PermanentError(memory.CodeConflict, "scope checkpoint moved backwards", nil)
		}

		if current.ScopeVersion == checkpoint.ScopeVersion && current.ChangeSeq == checkpoint.ChangeSeq &&
			current.CheckpointID == checkpoint.CheckpointID {
			return nil
		}
	} else if !privacyMissing(err) {
		return err
	}

	return putPrivacyRecord(ctx, tx, key, ProjectionCheckpoint, checkpoint)
}
