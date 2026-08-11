package memory

// SequencedTurn is one validated turn plus the producer-owned exact-scope FIFO
// cursor and a stable content fingerprint for replay/conflict checks.
type SequencedTurn struct {
	ProducerID    string
	ScopeSequence uint64
	Fingerprint   string
	Turn          Turn
}

// SequencedBoundary is one validated boundary plus the producer-owned
// exact-scope FIFO cursor and a stable content fingerprint.
type SequencedBoundary struct {
	ProducerID    string
	ScopeSequence uint64
	Fingerprint   string
	Boundary      Boundary
}

// IngestOutcome is the durable result of one accepted or replayed ordered
// ingest operation.
type IngestOutcome struct {
	SchemaVersion  string            `json:"schema_version"`
	Kind           IngestKind        `json:"kind"`
	Disposition    IngestDisposition `json:"disposition"`
	Projection     ProjectionStatus  `json:"projection_status"`
	ProducerID     string            `json:"producer_id"`
	Scope          Scope             `json:"scope"`
	ScopeSequence  uint64            `json:"scope_sequence"`
	Fingerprint    string            `json:"fingerprint"`
	ExportID       string            `json:"export_id"`
	OperationIDs   []string          `json:"operation_ids,omitempty"`
	ScopeVersion   uint64            `json:"scope_version"`
	ScopeChangeSeq uint64            `json:"scope_change_seq"`
}

// IngestSequenceCursor is the latest accepted producer/scope sequence.
type IngestSequenceCursor struct {
	SchemaVersion string `json:"schema_version"`
	ProducerID    string `json:"producer_id"`
	Scope         Scope  `json:"scope"`
	Latest        uint64 `json:"latest_sequence"`
}

// IngestSequenceRecord is the durable replay record for one producer/scope
// sequence and its accepted canonical outcome.
type IngestSequenceRecord struct {
	SchemaVersion string        `json:"schema_version"`
	ProducerID    string        `json:"producer_id"`
	Scope         Scope         `json:"scope"`
	Sequence      uint64        `json:"sequence"`
	Fingerprint   string        `json:"fingerprint"`
	Outcome       IngestOutcome `json:"outcome"`
}
