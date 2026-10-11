package badger

import (
	"context"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func normalizeCanonicalLogicalImportBatchSize(batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = defaultCanonicalImportBatch
	}

	if batchSize > maxCanonicalLogicalBatchSize {
		return 0, memory.PermanentError(memory.CodeLimitExceeded, "canonical logical import batch size is too large", nil)
	}

	return batchSize, nil
}

func (s *CanonicalStore) importCanonicalLogicalRecords(
	ctx context.Context,
	source io.Reader,
	batchSize int,
	progress func(CanonicalLogicalImportProgress) error,
) (CanonicalLogicalExportSummary, error) {
	var imported uint64

	summary, err := ScanCanonicalLogicalExport(ctx, source, batchSize, func(records []CanonicalLogicalExportRecord) error {
		if err := s.importCanonicalLogicalBatch(records); err != nil {
			return err
		}

		imported += uint64(len(records))

		return reportCanonicalLogicalImportProgress(progress, imported)
	})
	if err != nil {
		return CanonicalLogicalExportSummary{}, err
	}

	return summary, nil
}

func (s *CanonicalStore) importCanonicalLogicalBatch(records []CanonicalLogicalExportRecord) error {
	err := s.db.Update(func(txn *badger.Txn) error {
		return applyCanonicalLogicalImportBatch(txn, records)
	})
	if err != nil {
		return canonicalStoreError("import canonical logical records", err)
	}

	return nil
}

func reportCanonicalLogicalImportProgress(progress func(CanonicalLogicalImportProgress) error, imported uint64) error {
	if progress == nil {
		return nil
	}

	return progress(CanonicalLogicalImportProgress{ImportedRecords: imported})
}
