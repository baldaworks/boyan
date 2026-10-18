// Package archive defines backend-neutral canonical maintenance contracts and
// the application-owned SQL backup format. Checksums detect corruption, not
// authenticity. Restore implementations belong to their owning SQL store.
package archive

import (
	"context"
	"io"
)

// CanonicalBackupWriter writes opaque full (since=0) or incremental state.
// Reuse the returned end cursor verbatim for the next incremental backup.
type CanonicalBackupWriter interface {
	WriteCanonicalBackup(ctx context.Context, destination io.Writer, since uint64) (uint64, error)
}

// CanonicalBackupReader restores into a quiesced canonical store. Implemented
// by SQL stores in the restore lifecycle, separately from backup writing.
type CanonicalBackupReader interface {
	RestoreCanonicalBackup(ctx context.Context, source io.Reader, maxPendingWrites int) error
}

// CanonicalIntegrityVerifier validates stored envelopes and authority bindings.
type CanonicalIntegrityVerifier interface {
	VerifyCanonicalIntegrity(ctx context.Context) error
}

const (
	// SQLBackupFormat identifies length-prefixed JSON SQL backup frames.
	SQLBackupFormat = "boyan-sql-backup/v1"
	// MaxOpaqueFrameBytes bounds any opaque frame before allocation or IO.
	MaxOpaqueFrameBytes = 256 * 1024 * 1024
	// MaxValueBytes bounds a canonical record envelope.
	MaxValueBytes = 4 * 1024 * 1024
)

// BackupHeader is the first frame. SourceStoreUUID is hex encoded. All frames
// use a four-byte big-endian JSON byte length followed by exactly those bytes.
type BackupHeader struct {
	Kind            string `json:"kind"`
	Format          string `json:"format"`
	SourceStoreUUID string `json:"source_store_uuid"`
	Backend         string `json:"backend"`
	SchemaVersion   int    `json:"schema_version"`
	PrivacyProtocol int    `json:"privacy_protocol"`
	ProofProtocol   int    `json:"proof_protocol"`
	Mode            string `json:"mode"`
	Base            uint64 `json:"base"`
	End             uint64 `json:"end"`
}

// BackupRecord carries the original binary tuple and its latest stamp. Tuple
// components include scope key/kind for scoped families, and one empty byte
// component for global singleton families. No physical row/stamp ID is copied.
// Full archives retain deletion stamps as structural state, without old values.
type BackupRecord struct {
	Kind       string   `json:"kind"`
	Family     string   `json:"family"`
	Components [][]byte `json:"components"`
	Sequence   uint64   `json:"sequence"`
	Deleted    bool     `json:"deleted"`
	Envelope   []byte   `json:"envelope,omitempty"`
}

// BackupFooter terminates the stream. SHA256 covers every exact framed byte
// preceding this footer, including header and length prefixes. No trailing
// bytes are permitted. RecordCount includes live and deleted record frames.
type BackupFooter struct {
	Kind        string `json:"kind"`
	RecordCount uint64 `json:"record_count"`
	SHA256      string `json:"sha256"`
}
