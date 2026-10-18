package postgres

import (
	"context"
	"io"

	"github.com/baldaworks/boyan/store/archive"
)

var _ archive.CanonicalBackupWriter = (*Store)(nil)
var _ archive.CanonicalBackupReader = (*Store)(nil)
var _ archive.CanonicalIntegrityVerifier = (*Store)(nil)

// WriteCanonicalBackup streams a consistent full or incremental SQL archive.
func (s *Store) WriteCanonicalBackup(ctx context.Context, destination io.Writer, since uint64) (uint64, error) {
	return s.canonicalOwner().WriteCanonicalBackup(ctx, destination, since)
}

// VerifyCanonicalIntegrity validates envelopes and stored authority bindings.
func (s *Store) VerifyCanonicalIntegrity(ctx context.Context) error {
	return s.canonicalOwner().VerifyCanonicalIntegrity(ctx)
}

// RestoreCanonicalBackup loads a validated SQL archive behind durable control.
func (s *Store) RestoreCanonicalBackup(ctx context.Context, source io.Reader, pending int) error {
	return s.canonicalOwner().RestoreCanonicalBackup(ctx, source, pending)
}

// ResumeRestoreBeforeEraseRecovery finishes only a validated interrupted load.
func (s *Store) ResumeRestoreBeforeEraseRecovery(ctx context.Context) error {
	return s.canonicalOwner().ResumeRestoreBeforeEraseRecovery(ctx)
}
