package sqlite

import (
	"context"
	"database/sql"

	"github.com/baldaworks/boyan/pkg/memory"
)

// ReconcileProjection repairs clean canonical references before collecting any
// unreachable SQL rows. Process-owned builds and active/previous pointers are
// retained independently of canonical manifest enumeration.
func (a *SQLiteCanonicalApplier) ReconcileProjection(ctx context.Context) (result error) {
	return a.reconcileProjection(ctx, nil)
}

// Ordinary activation reconciles its full scope only. Startup retains the
// global repair-before-prune pass. The existing canonical walker still supplies
// previous-manifest authority; no cached or SQL-only retention proof replaces it.
func (a *SQLiteCanonicalApplier) reconcileProjection(ctx context.Context, scope *memory.Scope) (result error) {
	defer func() { result = lexicalStorageError(result) }()

	operation, done, err := a.acquire(ctx)
	if err != nil {
		return err
	}
	defer done()

	ctx = operation

	a.mu.Lock()
	walker, projectionID := a.retention, a.projectionID
	a.mu.Unlock()

	if walker == nil || projectionID != "sqlite" {
		return nil
	}

	retained, active, err := retainedProjectionManifests(ctx, walker, scope)
	if err != nil {
		return err
	}

	for _, manifest := range active {
		if err := a.reconcileActive(ctx, manifest); err != nil {
			return err
		}
	}

	keep := make(map[string]bool)

	for _, manifest := range retained {
		id, err := a.store.referencedBuildID(ctx, manifest)
		if err != nil {
			return err
		}

		if id != "" {
			keep[id] = true
		}
	}

	return a.pruneBuilds(ctx, keep, scope)
}

func (a *SQLiteCanonicalApplier) reconcileActive(ctx context.Context, manifest memory.ProjectionManifest) error {
	unlock, err := a.lockScope(ctx, manifest.Scope)
	if err != nil {
		return err
	}
	defer unlock()

	if a.foreignScopeOwner(manifest.Scope) {
		return nil
	}

	_, err = a.resumeLocked(ctx, manifest.Scope, manifest.GenerationID)

	return err
}

//nolint:cyclop // Validate every walked manifest before retaining exact scope and previous-generation authority.
func retainedProjectionManifests(ctx context.Context, walker projectionManifestWalker, scope *memory.Scope) (
	[]memory.ProjectionManifest, []memory.ProjectionManifest, error) {
	var keep, active []memory.ProjectionManifest

	previous := make(map[memory.Scope]memory.ProjectionManifest)

	err := walker.WalkProjectionManifests(ctx, "sqlite", func(manifest memory.ProjectionManifest, referenced bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		if manifest.Validate() != nil || manifest.ProjectionID != "sqlite" {
			return ErrCorrupt
		}

		if scope != nil && manifest.Scope != *scope {
			return nil
		}

		if referenced {
			keep = append(keep, manifest)
			if manifest.Status == memory.ProjectionGenerationActive {
				active = append(active, manifest)
			}
		}

		if manifest.Status == memory.ProjectionGenerationSuperseded {
			old := previous[manifest.Scope]
			if old.GenerationID == "" || old.UpdatedAt.Before(manifest.UpdatedAt) {
				previous[manifest.Scope] = manifest
			}
		}

		return nil
	})
	for _, manifest := range previous {
		keep = append(keep, manifest)
	}

	return keep, active, err
}

type physicalBuildIdentity struct {
	id         string
	scope      memory.Scope
	generation string
	watermark  uint64
}

func (a *SQLiteCanonicalApplier) pruneBuilds(ctx context.Context, retained map[string]bool, scope *memory.Scope) error {
	after := ""
	for {
		builds, err := a.store.buildPage(ctx, after, scope)
		if err != nil {
			return err
		}

		if len(builds) == 0 {
			return nil
		}

		for _, build := range builds {
			after = build.id

			unlock, err := a.lockScope(ctx, build.scope)
			if err != nil {
				return err
			}

			err = a.pruneBuild(ctx, build, retained)

			unlock()

			if err != nil {
				return err
			}
		}
	}
}

func (store *Store) buildPage(ctx context.Context, after string, scope *memory.Scope) ([]physicalBuildIdentity, error) {
	snapshot, err := store.read(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = snapshot.Close() }()

	query := `SELECT physical_build_id,scope_kind,scope_key,generation_id,watermark
 FROM lexical_build WHERE physical_build_id>?`
	args := []any{after}

	if scope != nil {
		query += ` AND scope_kind=? AND scope_key=?`

		args = append(args, scope.Kind, scope.Key)
	}

	query += ` ORDER BY physical_build_id LIMIT 128`

	rows, err := snapshot.tx.QueryContext(snapshot.ctx, query, args...)
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var result []physicalBuildIdentity

	for rows.Next() {
		var (
			build     physicalBuildIdentity
			watermark []byte
		)
		if err := rows.Scan(&build.id, &build.scope.Kind, &build.scope.Key, &build.generation, &watermark); err != nil {
			return nil, err
		}

		build.watermark, err = decodeUint64(watermark)
		if err != nil {
			return nil, err
		}

		result = append(result, build)
	}

	return result, rows.Err()
}

func (a *SQLiteCanonicalApplier) pruneBuild(ctx context.Context, build physicalBuildIdentity,
	retained map[string]bool) error {
	// Any live owner can be copying this build as its source. Protect the entire
	// scope until that owner publishes or aborts, without inspecting foreign state.
	a.store.stagingMu.Lock()
	owned := a.store.stagingOwners[build.scope] != nil
	a.store.stagingMu.Unlock()

	if owned {
		return nil
	}

	if retained[build.id] {
		return nil
	}

	canonical, found, err := a.store.canonicalReference(ctx, build.scope)
	if err != nil {
		return err
	}

	if found {
		id, err := a.store.referencedBuildID(ctx, canonical)
		if err != nil {
			return err
		}

		if id == build.id {
			return nil
		}
	}

	return a.deleteUnreachableBuild(ctx, build.id)
}

func (store *Store) referencedBuildID(ctx context.Context, manifest memory.ProjectionManifest) (string, error) {
	snapshot, err := store.read(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = snapshot.Close() }()

	physical, found, err := snapshot.committedLexical(manifest.Scope, manifest.GenerationID, manifest.Watermark, false)
	if err != nil || !found {
		return "", err
	}

	return physical.id, nil
}

// deleteUnreachableBuild runs under the scope lease. Record deletion pages are
// context aware and isolate oversized records using the shared input estimate;
// the final parent delete has no remaining child cascade.
func (a *SQLiteCanonicalApplier) deleteUnreachableBuild(ctx context.Context, id string) error {
	var reachable bool

	err := a.store.reader.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM lexical_active WHERE physical_build_id=?)
 OR EXISTS(SELECT 1 FROM lexical_previous WHERE physical_build_id=?)`, id, id).Scan(&reachable)
	if err != nil || reachable {
		return err
	}

	predicate := `b.physical_build_id=?`
	if err := a.scrubRows(ctx, predicate, []any{id}, false); err != nil {
		return err
	}

	return a.store.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM lexical_build WHERE physical_build_id=?
 AND NOT EXISTS(SELECT 1 FROM lexical_record WHERE physical_build_id=?)
 AND NOT EXISTS(SELECT 1 FROM lexical_active WHERE physical_build_id=?)
 AND NOT EXISTS(SELECT 1 FROM lexical_previous WHERE physical_build_id=?)`, id, id, id, id)

		return err
	})
}

// AbortProjectionSync can discard only this applier's owned unreachable work.
// In particular a canonical-first published build survives a failed SQL switch.
func (a *SQLiteCanonicalApplier) AbortProjectionSync(ctx context.Context, generation string) (result error) {
	defer func() { result = lexicalStorageError(result) }()

	operation, done, err := a.acquire(ctx)
	if err != nil {
		return err
	}
	defer done()

	ctx = operation

	a.mu.Lock()

	scopes := make([]memory.Scope, 0, len(a.stages))
	for scope, stage := range a.stages {
		if stage.generation == generation {
			scopes = append(scopes, scope)
		}
	}
	a.mu.Unlock()

	for _, scope := range scopes {
		unlock, err := a.lockScope(ctx, scope)
		if err != nil {
			return err
		}

		err = a.abortScope(ctx, scope, generation)

		unlock()

		if err != nil {
			return err
		}
	}

	return nil
}

func (a *SQLiteCanonicalApplier) abortScope(ctx context.Context, scope memory.Scope, generation string) error {
	a.mu.Lock()
	stage := a.stages[scope]
	a.mu.Unlock()

	if stage == nil || stage.generation != generation {
		return nil
	}

	retained, err := a.retainedStage(ctx, scope, stage)
	if err != nil {
		return err
	}

	if !retained {
		if err := a.deleteUnreachableBuild(ctx, stage.id); err != nil {
			return err
		}
	}

	a.releaseStage(scope, stage.id)

	return nil
}

//nolint:cyclop // Preserve enumerated and portable checkpoint references before releasing aborted ownership.
func (a *SQLiteCanonicalApplier) retainedStage(ctx context.Context,
	scope memory.Scope, stage *lexicalStage) (bool, error) {
	a.mu.Lock()
	walker, checkpoints := a.retention, a.checkpoints
	a.mu.Unlock()

	if walker != nil {
		retained, _, err := retainedProjectionManifests(ctx, walker, nil)
		if err != nil {
			return false, err
		}

		for _, manifest := range retained {
			id, err := a.store.referencedBuildID(ctx, manifest)
			if err != nil {
				return false, err
			}

			if id == stage.id {
				return true, nil
			}
		}
	}

	manifest, found, err := a.store.canonicalReference(ctx, scope)
	if err != nil {
		return false, err
	}

	if found && manifest.GenerationID == stage.generation {
		id, err := a.store.referencedBuildID(ctx, manifest)
		if err != nil || id == stage.id {
			return id == stage.id, err
		}
	}

	if walker != nil || checkpoints == nil {
		return false, nil
	}
	// A plain portable checkpoint adapter cannot enumerate which superseded
	// manifest is previous. Preserve a complete matching retained generation
	// conservatively; collection is disabled without enumeration anyway.
	manifest, found, err = checkpoints.LoadProjectionManifest(ctx, scope, "sqlite", stage.generation)
	if err != nil || !found {
		return false, err
	}

	if manifest.Validate() != nil || manifest.Scope != scope || manifest.ProjectionID != "sqlite" ||
		manifest.GenerationID != stage.generation {
		return false, ErrCorrupt
	}

	id, err := a.store.referencedBuildID(ctx, manifest)

	return id == stage.id, err
}
