package sqlite

import (
	"context"

	"github.com/baldaworks/boyan/pkg/memory"
)

// Called before creating incremental work, under the scope lease and a.mu.
// Replay from a retained watermark requires its complete physical base; a
// fresh database cannot replace that base with only the later deltas.
func (a *SQLiteCanonicalApplier) validateIncrementalBase(
	ctx context.Context, scope memory.Scope, generation string,
) error {
	if a.checkpoints == nil {
		return nil
	}

	manifest, found, err := a.checkpoints.LoadProjectionManifest(ctx, scope, "sqlite", generation)
	if err != nil || !found || manifest.Watermark == 0 {
		return err
	}

	snapshot, err := a.store.read(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = snapshot.Close() }()

	_, found, err = snapshot.committedLexical(scope, generation, manifest.Watermark, true)
	if err != nil {
		return err
	}

	if !found {
		return stagingConflict()
	}

	return nil
}

// A commit without Apply/Rebuild can copy a complete active source, or publish
// a proven empty initial scope. The target alone is never proof of emptiness.
func (a *SQLiteCanonicalApplier) validateEmptyCommit(ctx context.Context, manifest memory.ProjectionManifest) error {
	snapshot, err := a.store.read(ctx)
	if err != nil {
		return err
	}

	_, found, err := snapshot.committedLexical(manifest.Scope, manifest.GenerationID, manifest.Watermark, true)
	_ = snapshot.Close()

	if err != nil || found {
		return err
	}

	if manifest.Watermark != 0 {
		return stagingConflict()
	}

	sequence, err := a.reader.CurrentScopeChangeSeq(ctx, manifest.Scope)
	if err != nil {
		return err
	}

	if sequence != 0 {
		return stagingConflict()
	}

	return nil
}
