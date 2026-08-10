package memory

import "context"

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

// IngestSequenceStore persists exact-scope producer ordering and replay.
type IngestSequenceStore interface {
	LoadLatestIngestSequence(ctx context.Context, producerID string, scope Scope) (IngestSequenceCursor, bool, error)
	LoadIngestSequence(ctx context.Context, producerID string, scope Scope, sequence uint64) (IngestSequenceRecord, bool, error)
	SaveIngestSequence(ctx context.Context, record IngestSequenceRecord) error
}

func (k IngestKind) Validate() error {
	switch k {
	case IngestKindTurn, IngestKindBoundary:
		return nil
	default:
		return invalidDerived("ingest kind is invalid")
	}
}

func (d IngestDisposition) Validate() error {
	switch d {
	case IngestDispositionAccepted, IngestDispositionDuplicate:
		return nil
	default:
		return invalidDerived("ingest disposition is invalid")
	}
}

func (s ProjectionStatus) Validate() error {
	switch s {
	case ProjectionStatusCurrent, ProjectionStatusLagging:
		return nil
	default:
		return invalidDerived("projection status is invalid")
	}
}

func (r SequencedTurn) Validate() error {
	if !isCanonicalID(r.ProducerID) || !isCanonicalID(r.Fingerprint) || r.ScopeSequence == 0 {
		return invalidDerived("sequenced turn identity is invalid")
	}
	return r.Turn.Validate()
}

func (r SequencedBoundary) Validate() error {
	if !isCanonicalID(r.ProducerID) || !isCanonicalID(r.Fingerprint) || r.ScopeSequence == 0 {
		return invalidDerived("sequenced boundary identity is invalid")
	}
	return r.Boundary.Validate()
}

func (o IngestOutcome) Validate() error {
	if o.SchemaVersion != IngestOutcomeSchemaVersionV1 {
		return invalidDerived("ingest outcome schema version is invalid")
	}
	if err := o.Kind.Validate(); err != nil {
		return err
	}
	if err := o.Disposition.Validate(); err != nil {
		return err
	}
	if err := o.Projection.Validate(); err != nil {
		return err
	}
	if !isCanonicalID(o.ProducerID) || !isCanonicalID(o.Fingerprint) || !isCanonicalID(o.ExportID) || o.ScopeSequence == 0 || o.ScopeVersion == 0 || o.ScopeChangeSeq == 0 {
		return invalidDerived("ingest outcome identity is invalid")
	}
	if err := o.Scope.Validate(); err != nil {
		return err
	}
	return validateUniqueCanonicalIDs(o.OperationIDs, "ingest operation")
}

func (c IngestSequenceCursor) Validate() error {
	if c.SchemaVersion != IngestSequenceSchemaVersionV1 {
		return invalidDerived("ingest sequence cursor schema version is invalid")
	}
	if !isCanonicalID(c.ProducerID) || c.Latest == 0 {
		return invalidDerived("ingest sequence cursor identity is invalid")
	}
	return c.Scope.Validate()
}

func (r IngestSequenceRecord) Validate() error {
	if r.SchemaVersion != IngestSequenceSchemaVersionV1 {
		return invalidDerived("ingest sequence record schema version is invalid")
	}
	if !isCanonicalID(r.ProducerID) || !isCanonicalID(r.Fingerprint) || r.Sequence == 0 {
		return invalidDerived("ingest sequence record identity is invalid")
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if err := r.Outcome.Validate(); err != nil {
		return err
	}
	if r.Outcome.ProducerID != r.ProducerID || r.Outcome.Scope != r.Scope || r.Outcome.ScopeSequence != r.Sequence || r.Outcome.Fingerprint != r.Fingerprint {
		return invalidDerived("ingest sequence record outcome does not match its key")
	}
	if r.Outcome.Disposition != IngestDispositionAccepted {
		return invalidDerived("ingest sequence record must store an accepted outcome")
	}
	switch r.Outcome.Kind {
	case IngestKindTurn, IngestKindBoundary:
		return nil
	default:
		return invalidDerived("ingest sequence record kind is invalid")
	}
}
