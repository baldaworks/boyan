package archive

import memory "github.com/baldaworks/boyan/pkg/memory"

// CanonicalLogicalExportHeader identifies the application-owned JSONL format.
// Backend-native Badger keys, versions, and projection files are not exposed.
type CanonicalLogicalExportHeader struct {
	SchemaVersion string `json:"schema_version"`
	Kind          string `json:"kind"`
}

// CanonicalLogicalExportRecord is one portable tuple-key record. Envelope is
// retained as opaque validated bytes so storage encoding stays independent of
// the logical payload representation during export/import.
type CanonicalLogicalExportRecord struct {
	Kind       string   `json:"kind"`
	RecordType string   `json:"record_type"`
	Components []string `json:"components"`
	Suffix     []byte   `json:"suffix,omitempty"`
	Envelope   []byte   `json:"envelope"`
}

// CanonicalLogicalExportFooter checks the record stream and makes
// truncation detectable before import writes anything.
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
