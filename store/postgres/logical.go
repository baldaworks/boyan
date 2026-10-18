package postgres

import (
	"context"
	"io"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/store/archive"
)

var _ archive.CanonicalLogicalExporter = (*Store)(nil)
var _ archive.CanonicalLogicalImporter = (*Store)(nil)
var _ archive.CanonicalLogicalScanner = (*Store)(nil)

// ExportCanonicalLogical streams a consistent bounded SQL JSONL snapshot.
func (s *Store) ExportCanonicalLogical(ctx context.Context, destination io.Writer) error {
	return s.canonicalOwner().ExportCanonicalLogical(ctx, destination)
}

// ImportCanonicalLogical imports behind durable loading and privacy controls.
func (s *Store) ImportCanonicalLogical(ctx context.Context, source io.Reader) error {
	return s.canonicalOwner().ImportCanonicalLogical(ctx, source)
}

// ImportCanonicalLogicalWithOptions reports committed batches. Callbacks must
// not call this same owner while its maintenance admission is held.
func (s *Store) ImportCanonicalLogicalWithOptions(
	ctx context.Context,
	source io.Reader,
	options archive.CanonicalLogicalImportOptions,
) (archive.CanonicalLogicalExportSummary, error) {
	return s.canonicalOwner().ImportCanonicalLogicalWithOptions(ctx, source, options)
}

// ScanCanonicalLogicalExport validates independently of store readiness.
func (s *Store) ScanCanonicalLogicalExport(
	ctx context.Context,
	source io.Reader,
	batch int,
	onBatch func([]archive.CanonicalLogicalExportRecord) error,
) (archive.CanonicalLogicalExportSummary, error) {
	return ScanCanonicalLogicalExport(ctx, source, batch, onBatch)
}

// ScanCanonicalLogicalExport validates framing, exact identities and proofs.
// Consumers must await success before trusting the whole stream.
func ScanCanonicalLogicalExport(
	ctx context.Context,
	source io.Reader,
	batch int,
	onBatch func([]archive.CanonicalLogicalExportRecord) error,
) (archive.CanonicalLogicalExportSummary, error) {
	return canonicalsql.ScanCanonicalLogicalExport(ctx, source, batch, onBatch)
}
