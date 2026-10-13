package badger

import (
	"context"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ImportCanonicalLogical validates a logical export stream while applying it
// in bounded, idempotent transactions. A malformed footer still fails closed;
// callers that need restart-safe resume should persist external progress.
func (s *CanonicalStore) ImportCanonicalLogical(ctx context.Context, source io.Reader) error {
	_, err := s.ImportCanonicalLogicalWithOptions(ctx, source, CanonicalLogicalImportOptions{})

	return err
}

// ImportCanonicalLogicalWithOptions applies a validated logical export through
// bounded transactions and reports the validated stream summary.
func (s *CanonicalStore) ImportCanonicalLogicalWithOptions(
	ctx context.Context,
	source io.Reader,
	options CanonicalLogicalImportOptions,
) (CanonicalLogicalExportSummary, error) {
	if err := validateCanonicalMaintenance(ctx, source); err != nil {
		return CanonicalLogicalExportSummary{}, err
	}

	if s == nil {
		return CanonicalLogicalExportSummary{}, memory.PermanentError(
			memory.CodeStoreFailure,
			"canonical badger store is closed",
			nil,
		)
	}

	batchSize, err := normalizeCanonicalLogicalImportBatchSize(options.BatchSize)
	if err != nil {
		return CanonicalLogicalExportSummary{}, err
	}

	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()

	if s.db == nil {
		return CanonicalLogicalExportSummary{}, memory.PermanentError(
			memory.CodeStoreFailure,
			"canonical badger store is closed",
			nil,
		)
	}

	if err := s.db.View(func(txn *badger.Txn) error { return checkScopeErasePayloadGuard(txn) }); err != nil {
		return CanonicalLogicalExportSummary{}, canonicalStoreError("import while erase pending", err)
	}

	return s.importCanonicalLogicalRecords(ctx, source, batchSize, options.Progress)
}
