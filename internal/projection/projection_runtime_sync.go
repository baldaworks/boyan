package projection

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/baldaworks/boyan/internal/projectionsync"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Sync advances one exact-scope generation.
func (r *ProjectionRuntime) Sync(ctx context.Context, scope memory.Scope) error {
	if r == nil || r.coordinator == nil {
		return memory.PermanentError(memory.CodeDisabled, "projection runtime is unavailable", nil)
	}

	if ctx == nil {
		return memory.PermanentError(memory.CodeInvalidDerived, "projection runtime context is required", nil)
	}

	if err := scope.Validate(); err != nil {
		return err
	}

	entry, serial, err := r.admitScope(scope)
	if err != nil {
		return err
	}
	defer r.releaseScope(scope, entry)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-entry.gate:
	}

	defer func() { entry.gate <- struct{}{} }()

	return r.syncScope(ctx, scope, serial)
}

//nolint:cyclop // Keep resume, abort and replacement ownership transitions in order.
func (r *ProjectionRuntime) syncScope(ctx context.Context, scope memory.Scope, serial uint64) (result error) {
	generationID := ""
	defer func() {
		if result != nil && generationID != "" && r.maintenance != nil {
			result = errors.Join(result, r.maintenance.AbortProjectionSync(context.Background(), generationID))
		}
	}()

	resumedID, resumed, err := r.resumeScope(ctx, scope)
	if err != nil {
		return err
	}

	if resumed {
		generationID = resumedID
		_, err := r.coordinator.Sync(ctx, scope, r.projection, generationID)

		if !r.rebuildAfterSync(err) {
			return err
		}

		// The resumed delta can already own an unreachable physical stage.
		// Release it before acquiring this scope for the replacement generation.
		if r.maintenance != nil {
			if err := r.maintenance.AbortProjectionSync(ctx, generationID); err != nil {
				return err
			}
		}

		generationID = ""
	}

	generationID = projectionGenerationID(r.projection, scope, serial, time.Now().UTC())
	if r.starter != nil {
		if err := r.starter.BeginProjectionRebuild(ctx, scope, generationID); err != nil {
			return err
		}
	}

	if !r.rebuildable {
		_, err := r.coordinator.Sync(ctx, scope, r.projection, generationID)

		return err
	}

	_, err = r.coordinator.Rebuild(ctx, scope, r.projection, generationID)

	return err
}

func projectionGenerationID(projectionID string, scope memory.Scope, serial uint64, now time.Time) string {
	hash := sha256.Sum256(
		[]byte(
			fmt.Sprintf(
				"%s\x00%s\x00%s\x00%d\x00%d",
				projectionID,
				scope.Key,
				scope.Kind,
				serial,
				now.UnixNano(),
			),
		),
	)

	return memory.StableID("projection", hash[:])
}

func (r *ProjectionRuntime) admitScope(scope memory.Scope) (*scopeSync, uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closing {
		return nil, 0, memory.PermanentError(memory.CodeDisabled, "projection runtime is closing", nil)
	}

	entry := r.scopes[scope]
	if entry == nil {
		entry = &scopeSync{gate: make(chan struct{}, 1)}
		entry.gate <- struct{}{}

		r.scopes[scope] = entry
	}

	entry.users++
	r.serial++
	r.active.Add(1)

	return entry, r.serial, nil
}
func (r *ProjectionRuntime) releaseScope(scope memory.Scope, entry *scopeSync) {
	r.mu.Lock()

	entry.users--
	if entry.users == 0 {
		delete(r.scopes, scope)
	}
	r.mu.Unlock()
	r.active.Done()
}
func (r *ProjectionRuntime) resumeScope(ctx context.Context, scope memory.Scope) (string, bool, error) {
	if r.resumer == nil {
		return "", false, nil
	}

	manifest, found, err := r.checkpoints.LoadActiveProjectionManifest(ctx, scope, r.projection)
	if err != nil {
		return "", false, err
	}

	if found && manifest.Status == memory.ProjectionGenerationActive {
		valid, err := r.retainedManifest(ctx, manifest)
		if err != nil {
			return "", false, err
		}

		if valid {
			resumed, err := r.resumer.ResumeProjection(ctx, scope, manifest.GenerationID)

			return manifest.GenerationID, resumed, err
		}
	}

	return "", false, ctx.Err()
}
func (r *ProjectionRuntime) retainedManifest(ctx context.Context, manifest memory.ProjectionManifest) (bool, error) {
	retention, ok := r.canonical.(memory.ProjectionRetentionFloorReader)
	if !ok {
		return true, nil
	}

	floor, err := retention.LoadProjectionRetentionFloor(ctx, manifest.Scope, r.projection)

	return floor <= manifest.Watermark, err
}

func (r *ProjectionRuntime) rebuildAfterSync(err error) bool {
	return r.rebuildable && errors.Is(err, projectionsync.ErrProjectionRebuildRequired)
}
