package sqlite

import (
	"context"
	"database/sql"
	"slices"

	"github.com/baldaworks/boyan/pkg/memory"
)

// Commit cannot certify an inferred watermark. SQLite callers must use the
// exact-manifest capability before advancing the canonical checkpoint.
func (a *SQLiteCanonicalApplier) Commit(ctx context.Context, _ memory.Scope, _ string) error {
	_, done, err := a.acquire(ctx)
	if err != nil {
		return err
	}

	done()

	return stagingConflict()
}

// CommitProjectionManifest seals the explicitly supplied canonical replay
// target. SQL page commits never imply a completed projection certificate.
//
//nolint:cyclop // Exact identity, lifetime, ownership and sealing gates fail independently before certification.
func (a *SQLiteCanonicalApplier) CommitProjectionManifest(ctx context.Context,
	manifest memory.ProjectionManifest) (result error) {
	defer func() { result = lexicalStorageError(result) }()

	if err := manifest.Validate(); err != nil {
		return err
	}

	if manifest.ProjectionID != "sqlite" {
		return stagingConflict()
	}

	operation, done, err := a.acquire(ctx)
	if err != nil {
		return err
	}
	defer done()

	ctx = operation

	unlock, err := a.lockScope(ctx, manifest.Scope)
	if err != nil {
		return err
	}
	defer unlock()

	a.mu.Lock()
	stage := a.stages[manifest.Scope]
	a.mu.Unlock()

	if stage == nil {
		stage, err = a.stage(ctx, manifest.Scope, manifest.GenerationID, false)
		if err != nil {
			return err
		}
	}

	if stage == nil || stage.generation != manifest.GenerationID || stage.failed {
		return stagingConflict()
	}

	if err := a.copyActive(ctx, stage); err != nil {
		stage.failed = true

		return err
	}

	return a.sealStage(ctx, manifest, stage.id)
}

func (a *SQLiteCanonicalApplier) sealStage(ctx context.Context, manifest memory.ProjectionManifest, id string) error {
	return a.store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		const seal = `UPDATE lexical_build SET watermark=?,state='committed',
 record_count=(SELECT count(*) FROM lexical_record WHERE physical_build_id=?),
 source_count=(SELECT count(*) FROM lexical_source s JOIN lexical_record r ON r.rowid=s.record_rowid
 WHERE r.physical_build_id=?),
 conversation_count=(SELECT count(*) FROM lexical_conversation c JOIN lexical_record r ON r.rowid=c.record_rowid
 WHERE r.physical_build_id=?)
 WHERE physical_build_id=? AND scope_kind=? AND scope_key=? AND generation_id=? AND revoked=0
 AND schema_version=1 AND preprocessing_version=1
 AND NOT EXISTS(SELECT 1 FROM lexical_record r LEFT JOIN lexical_fts f ON f.rowid=r.rowid
 WHERE r.physical_build_id=? AND f.rowid IS NULL)`

		result, err := tx.ExecContext(ctx, seal, encodeUint64(manifest.Watermark), id, id, id,
			id, manifest.Scope.Kind, manifest.Scope.Key, manifest.GenerationID, id)
		if err != nil {
			return err
		}

		n, err := result.RowsAffected()
		if err != nil {
			return err
		}

		if n != 1 {
			return stagingConflict()
		}

		return nil
	})
}

var _ memory.ProjectionApplier = (*SQLiteCanonicalApplier)(nil)

func (a *SQLiteCanonicalApplier) beginBatch(ctx context.Context, stage *lexicalStage,
	revisions []memory.MemoryRevision) error {
	identities := make([]revisionIdentity, len(revisions))
	for i, revision := range revisions {
		identities[i] = revisionIdentity{revision.ItemID, revision.RevisionID, revision.Revision}
	}

	if stage.failed && !slices.Equal(stage.pending, identities) {
		return stagingConflict()
	}

	stage.failed = true
	stage.pending = identities

	return a.store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		const unseal = `UPDATE lexical_build SET state='building' WHERE physical_build_id=? AND revoked=0`

		result, err := tx.ExecContext(ctx, unseal, stage.id)
		if err != nil {
			return err
		}

		n, err := result.RowsAffected()
		if err != nil {
			return err
		}

		if n != 1 {
			return stagingConflict()
		}

		return nil
	})
}
