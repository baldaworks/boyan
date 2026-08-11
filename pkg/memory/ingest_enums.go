package memory

const (
	// IngestOutcomeSchemaVersionV1 identifies the durable ordered-ingest outcome.
	IngestOutcomeSchemaVersionV1 = "boyan-ingest-outcome/v1"
	// IngestSequenceSchemaVersionV1 identifies the durable producer/scope cursor.
	IngestSequenceSchemaVersionV1 = "boyan-ingest-sequence/v1"
)

// IngestKind identifies the accepted payload class at the process boundary.
type IngestKind string

const (
	IngestKindTurn     IngestKind = "turn"
	IngestKindBoundary IngestKind = "boundary"
)

// IngestDisposition identifies whether the returned outcome was newly accepted
// or replayed from durable producer/scope sequencing state.
type IngestDisposition string

const (
	IngestDispositionAccepted  IngestDisposition = "accepted"
	IngestDispositionDuplicate IngestDisposition = "duplicate"
)

// ProjectionStatus reports whether the rebuildable projection is current with
// the canonical commit associated with one ordered ingest result.
type ProjectionStatus string

const (
	ProjectionStatusCurrent ProjectionStatus = "current"
	ProjectionStatusLagging ProjectionStatus = "lagging"
)
