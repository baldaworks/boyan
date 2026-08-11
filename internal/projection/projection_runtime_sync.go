package projection

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

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
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return memory.PermanentError(memory.CodeDisabled, "projection runtime is closing", nil)
	}
	r.serial++
	serial := r.serial
	r.active.Add(1)
	r.mu.Unlock()
	defer r.active.Done()
	generationID := projectionGenerationID(r.projection, scope, serial, time.Now().UTC())
	if r.starter != nil {
		if err := r.starter.BeginProjectionRebuild(ctx, scope, generationID); err != nil {
			return err
		}
	}
	_, err := r.coordinator.Sync(ctx, scope, r.projection, generationID)
	return err
}

func projectionGenerationID(projectionID string, scope memory.Scope, serial uint64, now time.Time) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d", projectionID, scope.Key, scope.Kind, serial, now.UnixNano())))
	return memory.StableID("projection", hash[:])
}
