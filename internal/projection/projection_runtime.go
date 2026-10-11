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
	checkpoints memory.ProjectionCheckpointStore
	canonical   memory.CanonicalStore
	rebuildable bool
	resumer     projectionResumer
	scopes      map[memory.Scope]*scopeSync
	maintenance projectionMaintenance

	mu      sync.Mutex
	active  sync.WaitGroup
	serial  uint64
	closing bool
	drained chan struct{}
}

// NewProjectionRuntime constructs a projection coordinator over portable
// canonical and projection ports.
func NewProjectionRuntime(
	canonical memory.CanonicalStore,
	checkpoints memory.ProjectionCheckpointStore,
	applier memory.ProjectionApplier,
	projectionID string,
	batchSize uint32,
) (*ProjectionRuntime, error) {
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

	runtime := &ProjectionRuntime{
		coordinator: coordinator,
		projection:  projectionID,
		checkpoints: checkpoints,
		canonical:   canonical,
		scopes:      make(map[memory.Scope]*scopeSync),
	}
	runtime.configureAdapters(applier)

	return runtime, nil
}

var _ Syncer = (*ProjectionRuntime)(nil)
var _ Lifecycle = (*ProjectionRuntime)(nil)

type scopeSync struct {
	gate  chan struct{}
	users int
}

// Optional capabilities are kept separate from the portable projection ports.
type projectionResumer interface {
	ResumeProjection(ctx context.Context, scope memory.Scope, generationID string) (bool, error)
}
type projectionMaintenance interface {
	ReconcileProjection(ctx context.Context) error
	AbortProjectionSync(ctx context.Context, generationID string) error
}

func (runtime *ProjectionRuntime) configureAdapters(applier memory.ProjectionApplier) {
	if resumer, ok := applier.(projectionResumer); ok {
		runtime.resumer = resumer
	}

	_, batchRebuilder := applier.(memory.ProjectionBatchRebuildApplier)
	_, fullRebuilder := applier.(memory.ProjectionRebuildApplier)
	runtime.rebuildable = batchRebuilder || fullRebuilder

	if configurable, ok := applier.(interface {
		ConfigureProjectionRetention(checkpoints memory.ProjectionCheckpointStore, projectionID string)
	}); ok {
		configurable.ConfigureProjectionRetention(runtime.checkpoints, runtime.projection)
	}

	if maintenance, ok := applier.(projectionMaintenance); ok {
		runtime.maintenance = maintenance
	}

	if starter, ok := applier.(ProjectionRebuildStarter); ok {
		runtime.starter = starter
	}
}
