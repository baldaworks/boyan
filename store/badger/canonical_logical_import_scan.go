package badger

import (
	"context"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ScanCanonicalLogicalExport validates a full logical export stream, reports
// its footer metadata, and optionally processes validated records in batches.
func ScanCanonicalLogicalExport(ctx context.Context, source io.Reader, batchSize int, onBatch func([]CanonicalLogicalExportRecord) error) (CanonicalLogicalExportSummary, error) {
	if err := validateCanonicalMaintenance(ctx, source); err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	batchSize, err := normalizeCanonicalLogicalBatchSize(batchSize)
	if err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	scan := newCanonicalLogicalImportScanner(source)
	if err := readCanonicalLogicalExportHeader(&scan); err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	state := newCanonicalLogicalImportScanState(batchSize, onBatch)
	for {
		if err := checkCanonicalMaintenanceContext(ctx); err != nil {
			return CanonicalLogicalExportSummary{}, err
		}
		line, err := scan.readLine()
		if err != nil {
			return CanonicalLogicalExportSummary{}, err
		}
		kind, err := readCanonicalLogicalExportKind(&scan, line)
		if err != nil {
			return CanonicalLogicalExportSummary{}, err
		}
		switch kind {
		case canonicalLogicalRecordKind:
			if err := state.addRecord(&scan, line); err != nil {
				return CanonicalLogicalExportSummary{}, err
			}
		case canonicalLogicalFooterKind:
			return state.finish(&scan, line)
		default:
			return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeInvalidDerived, "canonical logical export record kind is invalid", nil)
		}
	}
}
