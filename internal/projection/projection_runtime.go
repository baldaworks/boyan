package projection

import (
	"context"
	"sync"

	projectionsync "github.com/baldaworks/boyan/internal/projectionsync"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

const defaultProjectionBatchSize = 128

// ProjectionRebuildStarter is an optional adapter hook used by projections
// that must create a disposable generation before replay begins.
type ProjectionRebuildStarter interface {
	BeginProjectionRebuild(ctx context.Context, scope memory.Scope, generationID string) error
}

// ProjectionRuntime coordinates rebuildable projection generations after
// canonical commits. It does not own canonical truth and never serves index
// records directly.
type ProjectionRuntime struct {
	coordinator *projectionsync.ProjectionCoordinator
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
	coordinator, err := projectionsync.NewProjectionCoordinator(canonical, checkpoints, applier, batchSize)
	if err != nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "create projection coordinator", err)
	}
	runtime := &ProjectionRuntime{coordinator: coordinator, projection: projectionID}
	if starter, ok := applier.(ProjectionRebuildStarter); ok {
		runtime.starter = starter
	}
	return runtime, nil
}

var _ Syncer = (*ProjectionRuntime)(nil)
var _ Lifecycle = (*ProjectionRuntime)(nil)
