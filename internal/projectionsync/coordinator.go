package projectionsync

import (
	"fmt"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

// ProjectionCoordinator replays canonical changes into one disposable
// projection generation. It never mutates canonical memory and never exposes a
// generation until the projection engine committed the corresponding watermark.
type ProjectionCoordinator struct {
	canonical   CanonicalStore
	checkpoints ProjectionCheckpointStore
	applier     ProjectionApplier
	batchSize   uint32
	now         func() time.Time
}

// NewProjectionCoordinator wires portable canonical and projection ports.
func NewProjectionCoordinator(
	canonical CanonicalStore,
	checkpoints ProjectionCheckpointStore,
	applier ProjectionApplier,
	batchSize uint32,
) (*ProjectionCoordinator, error) {
	if canonical == nil || checkpoints == nil || applier == nil {
		return nil, fmt.Errorf("canonical store, projection checkpoints, and applier are required")
	}

	if batchSize == 0 || batchSize > maxCanonicalReadRecords {
		return nil, fmt.Errorf("projection batch size must be between 1 and %d", maxCanonicalReadRecords)
	}

	return &ProjectionCoordinator{
		canonical:   canonical,
		checkpoints: checkpoints,
		applier:     applier,
		batchSize:   batchSize,
		now:         time.Now,
	}, nil
}
