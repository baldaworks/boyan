package memory

const (
	// DerivedSchemaVersionV1 identifies the first portable derived-memory schema.
	DerivedSchemaVersionV1 = "boyan-derived/v1"

	// MaxDerivedTurnTextBytes bounds raw message text accepted for derivation.
	MaxDerivedTurnTextBytes = 256 * 1024
	// MaxCandidateCount bounds model candidates returned by one stage.
	MaxCandidateCount = 32
	// MaxSourcesPerRevision bounds direct and parent provenance references.
	MaxSourcesPerRevision = 256
	// MaxDerivedTextBytes bounds any individual derived text field.
	MaxDerivedTextBytes = 32 * 1024
	// MaxSnapshotItems bounds a same-scope view supplied to a model.
	MaxSnapshotItems = 2048
	// MaxTraceNodes bounds one provenance traversal.
	MaxTraceNodes = 2048
	// MaxSearchResponseBytes bounds one derived search response.
	MaxSearchResponseBytes = 256 * 1024
)
