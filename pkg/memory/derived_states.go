package memory

// RevisionState identifies whether a derived revision participates in recall.
type RevisionState string

const (
	// RevisionStateActive participates in normal retrieval.
	RevisionStateActive RevisionState = "active"
	// RevisionStateSuperseded was replaced but remains in provenance history.
	RevisionStateSuperseded RevisionState = "superseded"
	// RevisionStateInvalidated cannot participate in normal retrieval.
	RevisionStateInvalidated RevisionState = "invalidated"
)

// SourceState identifies whether a raw source remains readable.
type SourceState string

const (
	// SourceStateActive may ground new and existing derived memory.
	SourceStateActive SourceState = "active"
	// SourceStateForgotten is an identity-only tombstone.
	SourceStateForgotten SourceState = "forgotten"
)
