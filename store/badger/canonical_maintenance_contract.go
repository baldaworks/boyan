package badger

import (
	"context"
	"io"
)

// CanonicalBackupWriter is the storage-neutral backup seam used by
// composition-root maintenance jobs. The bytes are Badger's application
// independent backup stream; callers must store them as an opaque artifact.
type CanonicalBackupWriter interface {
	WriteCanonicalBackup(ctx context.Context, destination io.Writer, since uint64) (uint64, error)
}

// CanonicalBackupReader restores one previously produced backup stream into an
// already opened, quiesced canonical store. Badger's transaction loader keeps
// the restore atomic per loaded batch and preserves its own record checksums.
type CanonicalBackupReader interface {
	RestoreCanonicalBackup(ctx context.Context, source io.Reader, maxPendingWrites int) error
}

// CanonicalIntegrityVerifier validates every stored canonical envelope without
// hydrating payload plaintext. It is intended for startup/restore diagnostics;
// malformed records fail closed and are never repaired automatically.
type CanonicalIntegrityVerifier interface {
	VerifyCanonicalIntegrity(ctx context.Context) error
}

var _ CanonicalBackupWriter = (*CanonicalStore)(nil)
var _ CanonicalBackupReader = (*CanonicalStore)(nil)
var _ CanonicalIntegrityVerifier = (*CanonicalStore)(nil)
