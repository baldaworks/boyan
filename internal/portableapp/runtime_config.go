package portableapp

import (
	"github.com/baldaworks/boyan/internal/structuredderive"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

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
	StructuredInvoker structuredderive.StructuredInvoker

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
