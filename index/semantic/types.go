// Package semantic owns the legacy disposable vector projection.
package semantic

import semanticcore "github.com/baldaworks/boyan/internal/semantic"

const schemaVersion = semanticcore.ProjectionSchemaVersion
const (
	StatusReady    = semanticcore.StatusReady
	StatusDegraded = semanticcore.StatusDegraded
)

type Status = semanticcore.Status
type View = semanticcore.View
type Generation = semanticcore.Generation
type OmittedCounts = semanticcore.OmittedCounts
type State = semanticcore.State
type CoverageRecord = semanticcore.CoverageRecord
type Publication = semanticcore.Publication
type Degraded = semanticcore.Degraded
type Chunk = semanticcore.ProjectionChunk

// Snapshot is an immutable in-memory copy of one validated generation.
type Snapshot struct {
	state  State
	chunks []Chunk
}

// State returns immutable metadata by value.
func (snapshot *Snapshot) State() State {
	if snapshot == nil {
		return State{}
	}

	return snapshot.state
}

// Chunks returns a deep copy so a caller cannot mutate the pinned view.
func (snapshot *Snapshot) Chunks() []Chunk {
	if snapshot == nil {
		return nil
	}

	return cloneChunks(snapshot.chunks)
}
