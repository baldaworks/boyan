package memory

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
	SchemaVersion  string            `json:"schema_version"`
	ExportID       string            `json:"export_id"`
	Disposition    IngestDisposition `json:"disposition"`
	Projection     ProjectionStatus  `json:"projection_status"`
	OperationID    string            `json:"operation_id,omitempty"`
	OperationIDs   []string          `json:"operation_ids,omitempty"`
	ScopeVersion   uint64            `json:"scope_version"`
	ScopeChangeSeq uint64            `json:"scope_change_seq"`
}

// IngestErrorResponse is the safe HTTP error body for malformed or failed ingest requests.
type IngestErrorResponse struct {
	Code         ErrorCode  `json:"code"`
	Class        ErrorClass `json:"class"`
	Message      string     `json:"message"`
	RetryAfterMS *int64     `json:"retry_after_ms,omitempty"`
}
