package badger

import "github.com/baldaworks/boyan/store/archive"

// CanonicalBackupWriter is the storage-neutral backup seam used by
// composition-root maintenance jobs. The bytes are Badger's application
// independent backup stream; callers must store them as an opaque artifact.
type CanonicalBackupWriter = archive.CanonicalBackupWriter

// CanonicalBackupReader restores one previously produced backup stream into an
// already opened, quiesced canonical store. Badger's transaction loader keeps
// the restore atomic per loaded batch and preserves its own record checksums.
type CanonicalBackupReader = archive.CanonicalBackupReader

// CanonicalIntegrityVerifier validates every stored canonical envelope without
// hydrating payload plaintext. It is intended for startup/restore diagnostics;
// malformed records fail closed and are never repaired automatically.
type CanonicalIntegrityVerifier = archive.CanonicalIntegrityVerifier

var _ CanonicalBackupWriter = (*CanonicalStore)(nil)
var _ CanonicalBackupReader = (*CanonicalStore)(nil)
var _ CanonicalIntegrityVerifier = (*CanonicalStore)(nil)
