package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func normalizeCanonicalLogicalBatchSize(batchSize int) (int, error) {
	if batchSize <= 0 {
		batchSize = defaultCanonicalImportBatch
	}

	if batchSize > maxCanonicalLogicalBatchSize {
		return 0, memory.PermanentError(memory.CodeLimitExceeded, "canonical logical batch size is too large", nil)
	}

	return batchSize, nil
}

func readCanonicalLogicalExportHeader(scan *canonicalLogicalImportScanner) error {
	var header CanonicalLogicalExportHeader
	if err := scan.readJSON(&header); err != nil {
		return err
	}

	if header.SchemaVersion != canonicalLogicalExportSchema ||
		header.Kind != canonicalLogicalHeaderKind {
		return memory.PermanentError(memory.CodeInvalidDerived, "canonical logical export header is invalid", nil)
	}

	return nil
}

func readCanonicalLogicalExportKind(scan *canonicalLogicalImportScanner, line []byte) (string, error) {
	var kind struct {
		Kind string `json:"kind"`
	}
	if err := scan.unmarshal(line, &kind); err != nil {
		return "", err
	}

	return kind.Kind, nil
}
