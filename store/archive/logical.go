package archive

import (
	"context"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const (
	// SQLLogicalFormat identifies the SQL JSONL stream, independently of Badger.
	SQLLogicalFormat = "boyan-sql-logical/v1"
	// MaxLogicalLineBytes includes the terminating newline. Binary envelopes
	// may need a smaller size to fit after base64 encoding and tuple metadata.
	MaxLogicalLineBytes = 4 * 1024 * 1024
)

// CanonicalLogicalExporter streams a complete application snapshot.
type CanonicalLogicalExporter interface {
	ExportCanonicalLogical(ctx context.Context, destination io.Writer) error
}

// CanonicalLogicalImporter requires a quiesced owner. Progress callbacks run
// after committed batches, while maintenance admission remains held. They must
// not recursively call the same owner; cancellation/error retains loading and
// requires a complete matching retry. A callback is not a readiness signal.
type CanonicalLogicalImporter interface {
	ImportCanonicalLogical(ctx context.Context, source io.Reader) error
	ImportCanonicalLogicalWithOptions(
		ctx context.Context,
		source io.Reader,
		options CanonicalLogicalImportOptions,
	) (CanonicalLogicalExportSummary, error)
}

// CanonicalLogicalScanner validates a stream without publishing readiness.
// Batch consumers see individually validated records before footer validation;
// they must wait for the successful summary before trusting the whole stream.
type CanonicalLogicalScanner interface {
	ScanCanonicalLogicalExport(
		ctx context.Context,
		source io.Reader,
		batch int,
		onBatch func([]CanonicalLogicalExportRecord) error,
	) (CanonicalLogicalExportSummary, error)
}

// CanonicalLogicalExportHeader identifies the application-owned JSONL format.
// Backend-native Badger keys, versions, and projection files are not exposed.
type CanonicalLogicalExportHeader struct {
	SchemaVersion      string `json:"schema_version"`
	Kind               string `json:"kind"`
	SourceStoreUUID    string `json:"source_store_uuid,omitempty"`
	Backend            string `json:"backend,omitempty"`
	StoreSchemaVersion int    `json:"store_schema_version,omitempty"`
	PrivacyProtocol    int    `json:"privacy_protocol,omitempty"`
	ProofProtocol      int    `json:"proof_protocol,omitempty"`
	Mode               string `json:"mode,omitempty"`
	Base               uint64 `json:"base,omitempty"`
	End                uint64 `json:"end,omitempty"`
}

// CanonicalLogicalExportRecord is one portable tuple-key record. Envelope is
// retained as opaque validated bytes so storage encoding stays independent of
// the logical payload representation during export/import.
type CanonicalLogicalExportRecord struct {
	Kind       string `json:"kind"`
	RecordType string `json:"record_type"`
	// SQLLogicalFormat encodes every original binary tuple component as base64.
	// RecordType is the family discriminator; no physical IDs are transferred.
	Components []string `json:"components"`
	Suffix     []byte   `json:"suffix,omitempty"`
	Envelope   []byte   `json:"envelope"`
	Sequence   uint64   `json:"sequence,omitempty"`
	Deleted    bool     `json:"deleted,omitempty"`
}

// CanonicalLogicalExportFooter checks the record stream and makes
// truncation detectable before trusted canonical record replay.
type CanonicalLogicalExportFooter struct {
	SchemaVersion string `json:"schema_version"`
	Kind          string `json:"kind"`
	RecordCount   uint64 `json:"record_count"`
	SHA256        string `json:"sha256"`
}

// CanonicalLogicalExportSummary reports validated stream metadata without
// exposing backend-native record layout. Scopes come only from canonical scope
// state records, so internal payload storage is excluded from rebuild inputs.
type CanonicalLogicalExportSummary struct {
	RecordCount uint64
	SHA256      string
	Scopes      []memory.Scope
}

// CanonicalLogicalImportOptions controls bounded, resumable import batches.
type CanonicalLogicalImportOptions struct {
	BatchSize int
	Progress  func(CanonicalLogicalImportProgress) error
}

// CanonicalLogicalImportProgress reports durable batch progress after one
// transaction committed.
type CanonicalLogicalImportProgress struct {
	ImportedRecords uint64
}
