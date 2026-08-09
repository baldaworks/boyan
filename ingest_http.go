package boyan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const (
	// IngestHTTPSchemaVersionV1 identifies the versioned HTTP ingest request envelope.
	IngestHTTPSchemaVersionV1 = "boyan-ingest/v1"
	// IngestReceiptSchemaVersionV1 identifies the HTTP ingest success response.
	IngestReceiptSchemaVersionV1 = "boyan-ingest-response/v1"
)

// IngestEnvelope is the strict HTTP/JSON request body for one ordered turn or
// boundary ingest request.
type IngestEnvelope[T any] struct {
	SchemaVersion string `json:"schema_version"`
	ProducerID    string `json:"producer_id"`
	ScopeSequence uint64 `json:"scope_sequence"`
	Payload       T      `json:"payload"`
}

// IngestReceipt is the safe HTTP success envelope returned to clients.
type IngestReceipt struct {
	SchemaVersion string            `json:"schema_version"`
	ExportID      string            `json:"export_id"`
	Disposition   IngestDisposition `json:"disposition"`
	Projection    ProjectionStatus  `json:"projection_status"`
	OperationID   string            `json:"operation_id,omitempty"`
	OperationIDs  []string          `json:"operation_ids,omitempty"`
	ScopeVersion  uint64            `json:"scope_version"`
	ScopeChangeSeq uint64           `json:"scope_change_seq"`
}

// IngestErrorResponse is the safe HTTP error body for malformed or failed ingest requests.
type IngestErrorResponse struct {
	Code         ErrorCode  `json:"code"`
	Class        ErrorClass `json:"class"`
	Message      string     `json:"message"`
	RetryAfterMS *int64     `json:"retry_after_ms,omitempty"`
}

func (e IngestEnvelope[T]) Validate(validatePayload func(T) error) error {
	if e.SchemaVersion != IngestHTTPSchemaVersionV1 {
		return invalidDerived("ingest envelope schema version is invalid")
	}
	if !isCanonicalID(e.ProducerID) || e.ScopeSequence == 0 {
		return invalidDerived("ingest envelope identity is invalid")
	}
	if validatePayload == nil {
		return invalidDerived("ingest payload validator is required")
	}
	return validatePayload(e.Payload)
}

func (r IngestReceipt) Validate() error {
	if r.SchemaVersion != IngestReceiptSchemaVersionV1 {
		return invalidDerived("ingest receipt schema version is invalid")
	}
	if !isCanonicalID(r.ExportID) || r.ScopeVersion == 0 || r.ScopeChangeSeq == 0 {
		return invalidDerived("ingest receipt identity is invalid")
	}
	if err := r.Disposition.Validate(); err != nil {
		return err
	}
	if err := r.Projection.Validate(); err != nil {
		return err
	}
	if r.OperationID != "" && !isCanonicalID(r.OperationID) {
		return invalidDerived("ingest receipt operation id is invalid")
	}
	return validateUniqueCanonicalIDs(r.OperationIDs, "ingest receipt operation")
}

func (r IngestErrorResponse) Validate() error {
	if r.Code == "" || r.Class == "" || r.Message == "" {
		return invalidDerived("ingest error response is invalid")
	}
	if r.RetryAfterMS != nil && *r.RetryAfterMS < 0 {
		return invalidDerived("ingest retry delay is invalid")
	}
	return nil
}

func TurnIngestFingerprint(envelope IngestEnvelope[Turn]) (string, error) {
	if err := envelope.Validate(func(turn Turn) error { return turn.Validate() }); err != nil {
		return "", err
	}
	return ingestPayloadFingerprint("turn", envelope.Payload)
}

func BoundaryIngestFingerprint(envelope IngestEnvelope[Boundary]) (string, error) {
	if err := envelope.Validate(func(boundary Boundary) error { return boundary.Validate() }); err != nil {
		return "", err
	}
	return ingestPayloadFingerprint("boundary", envelope.Payload)
}

func ingestPayloadFingerprint(kind string, payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", invalidDerived("ingest payload fingerprint is invalid")
	}
	sum := sha256.Sum256(append([]byte(kind+"\x00"), encoded...))
	return "boyan-ingest-fingerprint:" + hex.EncodeToString(sum[:]), nil
}

func IngestReceiptFromOutcome(outcome IngestOutcome) (IngestReceipt, error) {
	if err := outcome.Validate(); err != nil {
		return IngestReceipt{}, err
	}
	receipt := IngestReceipt{
		SchemaVersion:  IngestReceiptSchemaVersionV1,
		ExportID:       outcome.ExportID,
		Disposition:    outcome.Disposition,
		Projection:     outcome.Projection,
		OperationIDs:   append([]string(nil), outcome.OperationIDs...),
		ScopeVersion:   outcome.ScopeVersion,
		ScopeChangeSeq: outcome.ScopeChangeSeq,
	}
	if len(receipt.OperationIDs) == 1 {
		receipt.OperationID = receipt.OperationIDs[0]
	}
	if err := receipt.Validate(); err != nil {
		return IngestReceipt{}, err
	}
	return receipt, nil
}
