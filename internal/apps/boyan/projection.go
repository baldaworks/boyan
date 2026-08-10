package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const defaultProjectionBatchSize = 128

// ProjectionRebuildStarter is an optional adapter hook used by projections
// that must create a disposable generation before replay begins.
type ProjectionRebuildStarter interface {
	BeginProjectionRebuild(ctx context.Context, scope memory.Scope, generationID string) error
}

// ProjectionRuntime coordinates rebuildable projection generations after
// canonical commits.  It does not own canonical truth and never serves index
// records directly.
type ProjectionRuntime struct {
	coordinator *ProjectionCoordinator
	starter     ProjectionRebuildStarter
	projection  string

	mu      sync.Mutex
	active  sync.WaitGroup
	serial  uint64
	closing bool
}

// NewProjectionRuntime constructs a projection coordinator over portable
// canonical and projection ports.
func NewProjectionRuntime(canonical memory.CanonicalStore, checkpoints memory.ProjectionCheckpointStore, applier memory.ProjectionApplier, projectionID string, batchSize uint32) (*ProjectionRuntime, error) {
	if canonical == nil || checkpoints == nil || applier == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "projection runtime dependencies are required", nil)
	}
	if projectionID == "" {
		return nil, memory.PermanentError(memory.CodeInvalidDerived, "projection runtime identity is required", nil)
	}
	if batchSize == 0 {
		batchSize = defaultProjectionBatchSize
	}
	coordinator, err := NewProjectionCoordinator(canonical, checkpoints, applier, batchSize)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "create projection coordinator", err)
	}
	runtime := &ProjectionRuntime{coordinator: coordinator, projection: projectionID}
	if starter, ok := applier.(ProjectionRebuildStarter); ok {
		runtime.starter = starter
	}
	return runtime, nil
}

// Start satisfies Lifecycle.  Projection generations are started lazily per
// scope, after the canonical runtime is available.
func (*ProjectionRuntime) Start(context.Context) error { return nil }

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

// Close prevents new syncs and waits for in-flight projection work.
func (r *ProjectionRuntime) Close(context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.closing = true
	r.mu.Unlock()
	r.active.Wait()
	return nil
}

func projectionGenerationID(projectionID string, scope memory.Scope, serial uint64, now time.Time) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d", projectionID, scope.Key, scope.Kind, serial, now.UnixNano())))
	return "boyan-memory:v1:projection:" + hex.EncodeToString(hash[:])
}

var _ ProjectionSyncer = (*ProjectionRuntime)(nil)
var _ Lifecycle = (*ProjectionRuntime)(nil)
