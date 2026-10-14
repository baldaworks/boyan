package badger

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ExportCanonicalLogical writes a deterministic, backend-neutral JSONL
// export. It excludes disposable projection-generation records and includes
// payload blobs as opaque validated envelopes.
func (s *CanonicalStore) ExportCanonicalLogical(ctx context.Context, destination io.Writer) error {
	if err := validateCanonicalMaintenance(ctx, destination); err != nil {
		return err
	}

	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()

	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	if s.restoreBlocked.Load() {
		return restoreUnavailable()
	}

	if err := s.db.View(func(txn *badger.Txn) error { return verifyCanonicalIntegrityTxn(ctx, txn) }); err != nil {
		return err
	}

	writer := bufio.NewWriter(destination)
	if err := writeCanonicalLogicalJSON(writer, CanonicalLogicalExportHeader{
		SchemaVersion: canonicalLogicalExportSchema,
		Kind:          canonicalLogicalHeaderKind,
	}); err != nil {
		return err
	}

	hash := sha256.New()

	count, err := s.exportCanonicalLogicalRecords(ctx, writer, hash)
	if err != nil {
		return canonicalStoreError("export canonical logical records", err)
	}

	footer := CanonicalLogicalExportFooter{
		SchemaVersion: canonicalLogicalExportSchema,
		Kind:          canonicalLogicalFooterKind,
		RecordCount:   count,
		SHA256:        hex.EncodeToString(hash.Sum(nil)),
	}
	if err := writeCanonicalLogicalJSON(writer, footer); err != nil {
		return err
	}

	return writer.Flush()
}
