package app

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// TurnIngestor accepts a validated terminal turn.  The implementation owns
// semantic extraction, evidence grounding, temporal interpretation, and the
// idempotent canonical commit.
type TurnIngestor interface {
	IngestTurn(ctx context.Context, turn memory.Turn) error
}

// BoundaryIngestor applies a validated session boundary.  Boundary synthesis
// is kept separate from turn ingestion so hosts can grant only the capability
// needed by a worker.
type BoundaryIngestor interface {
	ApplyBoundary(ctx context.Context, boundary memory.Boundary) error
}

// SequencedTurnIngestor accepts one producer-ordered terminal turn and returns
// the durable ordered-ingest outcome.
type SequencedTurnIngestor interface {
	IngestSequencedTurn(ctx context.Context, request memory.SequencedTurn) (memory.IngestOutcome, error)
}

// SequencedBoundaryIngestor accepts one producer-ordered boundary and returns
// the durable ordered-ingest outcome.
type SequencedBoundaryIngestor interface {
	ApplySequencedBoundary(ctx context.Context, request memory.SequencedBoundary) (memory.IngestOutcome, error)
}

// RecallReader exposes bounded, exact-scope canonical recall.
type RecallReader interface {
	Search(ctx context.Context, request memory.RecallRequest) (memory.RecallResponse, error)
}

// TraceReader exposes bounded provenance inspection for one exact scope.
type TraceReader interface {
	Trace(ctx context.Context, request memory.TraceRequest) (memory.TraceResponse, error)
}

// Forgetter is intentionally a Go application capability.  The portable MCP
// adapter does not expose either mutating operation.
type Forgetter interface {
	ForgetSource(ctx context.Context, command memory.ForgetSourceCommand) (memory.ForgetOutcome, error)
	ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error)
}

// Lifecycle is implemented by resources that need ordered startup and
// shutdown.  A nil Lifecycle is never accepted by Runtime.
type Lifecycle interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
}

// StructuredInvocation is the neutral model boundary.  Operation identity,
// stage, input, and output schema are supplied by the application; provider
// credentials, retries, and runtime lifetime remain implementation-owned.
type StructuredInvocation struct {
	OperationID  string
	Stage        string
	Instruction  string
	InputJSON    []byte
	OutputSchema string
}

// StructuredInvoker performs one bounded structured invocation.
type StructuredInvoker interface {
	Invoke(ctx context.Context, invocation StructuredInvocation) ([]byte, error)
	Close(ctx context.Context) error
}

// BoundaryExecutor is the application-owned boundary semantic port.  Hosts
// may provide a processor backed by the canonical store, while the runtime
// still owns capability composition and projection ordering.
type BoundaryExecutor interface {
	ProcessBoundary(ctx context.Context, boundary memory.Boundary) (memory.BoundaryOutcome, error)
}

// ProjectionSyncer advances a rebuildable projection after a canonical commit.
// Implementations must keep canonical state authoritative when projection
// work fails.
type ProjectionSyncer interface {
	Sync(ctx context.Context, scope memory.Scope) error
}

// RuntimeConfig wires the portable runtime.  The first group is sufficient to
// construct turn ingestion.  Recall, boundary, trace, and forget are supplied
// through their narrow ports or are built from the corresponding canonical
// ports when all dependencies are present.
type RuntimeConfig struct {
	CanonicalStore memory.CanonicalStore
	Extractor      memory.CanonicalSemanticExtractor
	Policy         memory.PolicyRegistry
	Derivation     memory.DerivationRef

	// StructuredInvoker is used to construct the portable Deriver when
	// Extractor is nil.  It is never inspected for a concrete provider type.
	StructuredInvoker StructuredInvoker

	Boundary BoundaryExecutor
	Recall   RecallReader
	Trace    TraceReader
	Forget   Forgetter
	Project  ProjectionSyncer

	// Canonical recall ports build RecallService when Recall is nil.
	RecallCanonical  memory.RecallCanonicalReader
	RecallProjection memory.RecallProjection

	// Optional canonical projection wiring.  When all fields are supplied,
	// NewRuntime builds a portable ProjectionRuntime.
	ProjectionCheckpoints memory.ProjectionCheckpointStore
	ProjectionApplier     memory.ProjectionApplier
	ProjectionID          string
	ProjectionBatchSize   uint32

	// CanonicalDerived is used for the transport-neutral trace adapter when
	// Trace is nil.  It is intentionally a read-only port.
	CanonicalDerived memory.CanonicalDerivedReader

	// Optional lifecycle dependencies are started in declaration order and
	// closed in reverse order.  The runtime closes resources it started when a
	// later Start call fails.
	Lifecycle []Lifecycle
}
