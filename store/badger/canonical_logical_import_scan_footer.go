package badger

import (
	"encoding/hex"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *canonicalLogicalImportScanState) finish(scan *canonicalLogicalImportScanner, line []byte) (CanonicalLogicalExportSummary, error) {
	var footer CanonicalLogicalExportFooter
	if err := scan.unmarshal(line, &footer); err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	if footer.SchemaVersion != canonicalLogicalExportSchema ||
		footer.Kind != canonicalLogicalFooterKind ||
		footer.RecordCount != s.count ||
		footer.SHA256 != hex.EncodeToString(s.hash.Sum(nil)) {
		return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeInvalidDerived, "canonical logical export footer does not match records", nil)
	}
	if err := s.batches.flush(); err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	if scan.hasTrailingRecord() {
		return CanonicalLogicalExportSummary{}, memory.PermanentError(memory.CodeInvalidDerived, "canonical logical export has trailing records", nil)
	}
	if err := scan.err(); err != nil {
		return CanonicalLogicalExportSummary{}, err
	}
	return CanonicalLogicalExportSummary{
		RecordCount: footer.RecordCount,
		SHA256:      footer.SHA256,
		Scopes:      sortedCanonicalLogicalScopes(s.scopes),
	}, nil
}
