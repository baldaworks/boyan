package sqlite

import (
	"context"
	"database/sql"

	"github.com/baldaworks/boyan/pkg/memory"
)

type projectionManifestWalker interface {
	WalkProjectionManifests(ctx context.Context, projectionID string,
		visit func(manifest memory.ProjectionManifest, referenced bool) error) error
}

// ConfigureProjectionRetention supplies canonical authority as well as the
// optional enumeration port. Without authority, no physical view is certified.
func (a *SQLiteCanonicalApplier) ConfigureProjectionRetention(checkpoints memory.ProjectionCheckpointStore,
	projectionID string) {
	a.mu.Lock()
	a.checkpoints = checkpoints
	a.projectionID = projectionID
	a.retention, _ = checkpoints.(projectionManifestWalker)
	a.mu.Unlock()
	a.store.mu.Lock()
	if projectionID == "sqlite" {
		a.store.checkpoints = checkpoints
	} else {
		a.store.checkpoints = nil
	}
	a.store.mu.Unlock()
}

func (a *SQLiteCanonicalApplier) Activate(ctx context.Context, scope memory.Scope, generation string) error {
	resumed, err := a.ResumeProjection(ctx, scope, generation)
	if err != nil {
		return err
	}

	if !resumed {
		return stagingConflict()
	}

	return a.reconcileProjection(ctx, &scope)
}

// ResumeProjection repairs the canonical-first activation crash window only
// when the exact clean checkpoint exists physically and remains replayable.
func (a *SQLiteCanonicalApplier) ResumeProjection(ctx context.Context, scope memory.Scope,
	generation string) (resumed bool, result error) {
	defer func() { result = lexicalStorageError(result) }()

	operation, done, err := a.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer done()

	ctx = operation

	unlock, err := a.lockScope(ctx, scope)
	if err != nil {
		return false, err
	}
	defer unlock()

	return a.resumeLocked(ctx, scope, generation)
}

func (a *SQLiteCanonicalApplier) resumeLocked(ctx context.Context, scope memory.Scope,
	generation string) (bool, error) {
	manifest, found, err := a.store.canonicalManifest(ctx, scope)
	if err != nil || !found || manifest.GenerationID != generation {
		return false, err
	}

	a.mu.Lock()
	checkpoints := a.checkpoints
	a.mu.Unlock()

	if retention, ok := checkpoints.(memory.ProjectionRetentionFloorReader); ok {
		floor, err := retention.LoadProjectionRetentionFloor(ctx, scope, "sqlite")
		if err != nil || floor > manifest.Watermark {
			return false, err
		}
	}

	return a.activateManifest(ctx, manifest)
}

// Called under the shared scope lease, including when reconciliation repairs a
// retained reference. No canonical I/O is performed while holding a SQL writer.
//
//nolint:cyclop // Physical and canonical gates plus atomic pointer failures are explicit under one scope lease.
func (a *SQLiteCanonicalApplier) activateManifest(ctx context.Context,
	manifest memory.ProjectionManifest) (bool, error) {
	snapshot, err := a.store.read(ctx)
	if err != nil {
		return false, err
	}

	a.mu.Lock()
	stage := a.stages[manifest.Scope]
	a.mu.Unlock()

	var preferred []string
	if stage != nil && stage.generation == manifest.GenerationID {
		preferred = []string{stage.id}
	}

	physical, found, err := snapshot.committedLexical(
		manifest.Scope, manifest.GenerationID, manifest.Watermark, false, preferred...)
	if err == nil && !found && len(preferred) > 0 {
		physical, found, err = snapshot.committedLexical(manifest.Scope, manifest.GenerationID, manifest.Watermark, true)
	}

	if err == nil && found {
		err = a.checkActivationOwner(snapshot, manifest.Scope, physical.id)
	}

	_ = snapshot.Close()

	if err != nil || !found {
		return false, err
	}

	after, valid, err := a.store.canonicalManifest(ctx, manifest.Scope)
	if err != nil || !valid || !sameProjectionTuple(manifest, after) {
		return false, err
	}

	err = a.store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		const previous = `INSERT INTO lexical_previous(scope_kind,scope_key,physical_build_id)
 SELECT scope_kind,scope_key,physical_build_id FROM lexical_active
 WHERE scope_kind=? AND scope_key=? AND physical_build_id<>?
 ON CONFLICT(scope_kind,scope_key) DO UPDATE SET physical_build_id=excluded.physical_build_id`
		if _, err := tx.ExecContext(ctx, previous, manifest.Scope.Kind, manifest.Scope.Key, physical.id); err != nil {
			return err
		}

		const activate = `INSERT INTO lexical_active(scope_kind,scope_key,physical_build_id) VALUES(?,?,?)
 ON CONFLICT(scope_kind,scope_key) DO UPDATE SET physical_build_id=excluded.physical_build_id`

		_, err := tx.ExecContext(ctx, activate, manifest.Scope.Kind, manifest.Scope.Key, physical.id)

		return err
	})
	if err != nil {
		return false, err
	}

	a.releaseStage(manifest.Scope, physical.id)

	return true, nil
}

func (a *SQLiteCanonicalApplier) foreignScopeOwner(scope memory.Scope) bool {
	a.store.stagingMu.Lock()
	defer a.store.stagingMu.Unlock()

	owner := a.store.stagingOwners[scope]

	return owner != nil && owner != a
}

// A live foreign stage must never become active while its owner can still
// mutate it. Idempotently keeping an already-active immutable source is safe.
func (a *SQLiteCanonicalApplier) checkActivationOwner(snapshot *readSnapshot,
	scope memory.Scope, id string) error {
	if !a.foreignScopeOwner(scope) {
		return nil
	}

	active, err := snapshot.activeLexicalBuild(scope)
	if err != nil {
		return err
	}

	if active != id {
		return stagingConflict()
	}

	return nil
}

func (a *SQLiteCanonicalApplier) releaseStage(scope memory.Scope, id string) {
	a.mu.Lock()
	if stage := a.stages[scope]; stage != nil && stage.id == id {
		delete(a.stages, scope)
		a.store.stagingMu.Lock()
		if a.store.stagingOwners[scope] == a {
			delete(a.store.stagingOwners, scope)
		}
		a.store.stagingMu.Unlock()
	}
	a.mu.Unlock()
}

var _ memory.ProjectionGenerationActivator = (*SQLiteCanonicalApplier)(nil)
